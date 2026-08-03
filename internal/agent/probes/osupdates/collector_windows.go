//go:build windows

package osupdates

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows/registry"

	"senhub-agent.go/internal/agent/services/logger"
)

// securityCategoryID is the fixed WUA category GUID for "Security
// Updates" (documented in the IUpdate::Categories reference).
const securityCategoryID = "0FA1201D-4330-4FA8-8AE9-B877473B6441"

// HRESULTs tolerated from CoInitializeEx: S_FALSE (COM already
// initialised on this thread) and RPC_E_CHANGED_MODE (initialised with
// a different concurrency model). COM is usable in both cases.
const (
	hrSFalse          = 0x00000001
	hrRPCEChangedMode = 0x80010106
)

// wuaCollector queries the Windows Update Agent COM API:
// Microsoft.Update.Session → CreateUpdateSearcher →
// Search("IsInstalled=0 ...") for the pending counts, and
// Microsoft.Update.SystemInfo.RebootRequired for the reboot flag.
type wuaCollector struct {
	logger *logger.ModuleLogger
}

func newOSUpdatesCollector(moduleLogger *logger.ModuleLogger) updatesCollector {
	return &wuaCollector{logger: moduleLogger}
}

// collect performs one WUA query. The Search call is synchronous COM
// and does not honour ctx — the WUA service bounds it internally; on a
// host that has never scanned, the first search can take minutes. Any
// COM failure surfaces as an error so the probe emits
// senhub.os.updates.up=0 for the cycle.
func (c *wuaCollector) collect(_ context.Context) (updatesStatus, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	needUninit := true
	if initErr := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); initErr != nil {
		var oleErr *ole.OleError
		if !errors.As(initErr, &oleErr) {
			return updatesStatus{}, fmt.Errorf("CoInitializeEx: %w", initErr)
		}
		switch oleErr.Code() {
		case hrSFalse:
			// Already initialised on this thread; still balanced by
			// CoUninitialize below.
		case hrRPCEChangedMode:
			// A different concurrency model owns this thread — usable,
			// but not ours to uninitialise.
			needUninit = false
		default:
			return updatesStatus{}, fmt.Errorf("CoInitializeEx: %w", initErr)
		}
	}
	if needUninit {
		defer ole.CoUninitialize()
	}

	status := updatesStatus{packageManager: "wua"}

	pending, security, err := c.searchPendingUpdates()
	if err != nil {
		return updatesStatus{}, err
	}
	status.pending, status.pendingSecurity = pending, security

	// Reboot detection is best-effort: a failure here must not discard the
	// pending/security counts already collected (the probe stays up=1).
	if reboot, rerrr := c.rebootRequired(); rerrr != nil {
		c.logger.Warn().Err(rerrr).Msg("reboot-required check failed; reporting reboot_required=0")
	} else {
		status.rebootRequired = reboot
	}
	return status, nil
}

func (c *wuaCollector) searchPendingUpdates() (int, int, error) {
	session, err := createDispatch("Microsoft.Update.Session")
	if err != nil {
		return 0, 0, err
	}
	defer session.Release()

	searcherRaw, err := oleutil.CallMethod(session, "CreateUpdateSearcher")
	if err != nil {
		return 0, 0, fmt.Errorf("CreateUpdateSearcher: %w", err)
	}
	searcher := searcherRaw.ToIDispatch()
	defer searcher.Release()

	resultRaw, err := oleutil.CallMethod(searcher, "Search", "IsInstalled=0 and IsHidden=0 and Type='Software'")
	if err != nil {
		return 0, 0, fmt.Errorf("IUpdateSearcher.Search: %w", err)
	}
	result := resultRaw.ToIDispatch()
	defer result.Release()

	updatesRaw, err := oleutil.GetProperty(result, "Updates")
	if err != nil {
		return 0, 0, fmt.Errorf("ISearchResult.Updates: %w", err)
	}
	updates := updatesRaw.ToIDispatch()
	defer updates.Release()

	countRaw, err := oleutil.GetProperty(updates, "Count")
	if err != nil {
		return 0, 0, fmt.Errorf("IUpdateCollection.Count: %w", err)
	}
	count := int(countRaw.Val)

	security := 0
	for i := 0; i < count; i++ {
		isSec, err := c.isSecurityUpdate(updates, i)
		if err != nil {
			c.logger.Warn().Int("index", i).Err(err).
				Msg("classifying pending update failed; not counted as security")
			continue
		}
		if isSec {
			security++
		}
	}
	return count, security, nil
}

// isSecurityUpdate marks an update as security when its MsrcSeverity is
// set (Critical/Important/Moderate/Low — only security bulletins carry
// one) or when its categories include the "Security Updates" category.
func (c *wuaCollector) isSecurityUpdate(updates *ole.IDispatch, index int) (bool, error) {
	// Item is an indexed PROPERTY (DISPATCH_PROPERTYGET), not a method —
	// CallMethod raises "Member not found" against IUpdateCollection.
	itemRaw, err := oleutil.GetProperty(updates, "Item", index)
	if err != nil {
		return false, fmt.Errorf("IUpdateCollection.Item(%d): %w", index, err)
	}
	item := itemRaw.ToIDispatch()
	defer item.Release()

	if sevRaw, sevErr := oleutil.GetProperty(item, "MsrcSeverity"); sevErr == nil {
		if sev, ok := sevRaw.Value().(string); ok && strings.TrimSpace(sev) != "" {
			return true, nil
		}
	}

	catsRaw, err := oleutil.GetProperty(item, "Categories")
	if err != nil {
		return false, fmt.Errorf("IUpdate.Categories: %w", err)
	}
	cats := catsRaw.ToIDispatch()
	defer cats.Release()

	catCountRaw, err := oleutil.GetProperty(cats, "Count")
	if err != nil {
		return false, fmt.Errorf("ICategoryCollection.Count: %w", err)
	}
	for j := 0; j < int(catCountRaw.Val); j++ {
		catRaw, catErr := oleutil.GetProperty(cats, "Item", j)
		if catErr != nil {
			continue
		}
		cat := catRaw.ToIDispatch()
		catID := ""
		if idRaw, idErr := oleutil.GetProperty(cat, "CategoryID"); idErr == nil {
			catID, _ = idRaw.Value().(string)
		}
		cat.Release()
		if strings.EqualFold(catID, securityCategoryID) {
			return true, nil
		}
	}
	return false, nil
}

// rebootRequired reports a pending OS reboot from the documented Windows
// registry indicators. Unlike Microsoft.Update.SystemInfo (a COM object whose
// instantiation is denied without an elevated token), these keys are readable
// from any context, so the signal is reliable when the agent runs as a service
// AND when it is exercised interactively. Any indicator present ⇒ reboot pending.
func (c *wuaCollector) rebootRequired() (bool, error) {
	for _, path := range []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`,
	} {
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE); err == nil {
			_ = k.Close()
			return true, nil
		}
	}
	// A queued file rename is the classic "reboot to finish servicing" signal.
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Session Manager`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if vals, _, err := k.GetStringsValue("PendingFileRenameOperations"); err == nil && len(vals) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func createDispatch(progID string) (*ole.IDispatch, error) {
	unknown, err := oleutil.CreateObject(progID)
	if err != nil {
		return nil, fmt.Errorf("CreateObject(%s): %w", progID, err)
	}
	defer unknown.Release()

	dispatch, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, fmt.Errorf("QueryInterface(%s, IDispatch): %w", progID, err)
	}
	return dispatch, nil
}
