//go:build windows

package hyperv

import (
	"encoding/xml"

	"github.com/yusufpapurcu/wmi"
)

// msvmKvpExchangeComponentGuestIntrinsic is the WMI projection of
// Msvm_KvpExchangeComponentSettingData for guest intrinsic items. The
// GuestIntrinsicExchangeItems field holds a slice of XML strings, each
// describing one key-value pair the Hyper-V Integration Services push from
// the guest to the hypervisor. The MachineId item carries the guest OS
// machine-id (stable across rename, equivalent to /etc/machine-id on Linux
// or MachineGuid on Windows).
type msvmKvpExchangeComponentGuestIntrinsic struct {
	GuestIntrinsicExchangeItems []string
}

// kvpItem is the XML shape of one Hyper-V KVP exchange item.
type kvpItem struct {
	Name  string `xml:"Name"`
	Value string `xml:"Data"`
}

// kvpGuestMachineID queries the Hyper-V KVP data exchange channel to obtain
// the guest's machine-id for the given VM GUID. Returns "" when the KVP
// channel is unavailable (Integration Services not installed, VM off, or WMI
// query error).
//
// Msvm_KvpExchangeComponentSettingData with InstanceID matching the VM GUID
// holds GuestIntrinsicExchangeItems — a slice of XML-encoded key/value pairs
// pushed by Hyper-V Integration Services. The item named "MachineId" is the
// guest OS machine-id (the same value as /etc/machine-id on Linux or
// HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid on Windows guests).
func kvpGuestMachineID(vmGUID string) string {
	query := `SELECT GuestIntrinsicExchangeItems FROM Msvm_KvpExchangeComponentSettingData WHERE InstanceID LIKE '%` + vmGUID + `%'`

	var rows []msvmKvpExchangeComponentGuestIntrinsic
	if err := wmi.QueryNamespace(query, &rows, hypervNamespace); err != nil || len(rows) == 0 {
		return ""
	}

	for _, row := range rows {
		for _, xmlStr := range row.GuestIntrinsicExchangeItems {
			if xmlStr == "" {
				continue
			}
			var item kvpItem
			if err := xml.Unmarshal([]byte(xmlStr), &item); err != nil {
				continue
			}
			if item.Name == "MachineId" && item.Value != "" {
				return item.Value
			}
		}
	}
	return ""
}
