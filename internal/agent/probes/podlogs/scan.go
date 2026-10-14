package podlogs

import (
	"fmt"
	"io/fs"
	"sort"
	"time"
)

type FileState struct {
	Path    string
	Ref     PodRef
	Size    int64
	ModTime time.Time
	Inode   uint64
}

type Scan struct {
	Files   []FileState
	Rotated []FileState
}

type ChangeKind string

const (
	Added     ChangeKind = "Added"
	Removed   ChangeKind = "Removed"
	Rotated   ChangeKind = "Rotated"
	Truncated ChangeKind = "Truncated"
)

type Change struct {
	Kind   ChangeKind
	Before FileState
	After  FileState
}

func ScanPods(fsys fs.FS, root string, f Filter, self PodRef) (Scan, error) {
	rootInfo, err := fs.Stat(fsys, root)
	if err != nil {
		return Scan{}, err
	}
	if !rootInfo.IsDir() {
		return Scan{}, fmt.Errorf("podlogs: %q is not a directory", root)
	}

	var files []FileState
	err = fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == root {
				return walkErr
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ref, ok := ParsePodLogPath(p)
		if !ok {
			return nil
		}
		fi, statErr := fs.Stat(fsys, p)
		if statErr != nil {
			return nil
		}
		if !fi.Mode().IsRegular() || fi.Mode()&fs.ModeSymlink != 0 {
			return nil
		}
		if !f.Allows(ref) {
			return nil
		}
		if self != (PodRef{}) && ref.Namespace == self.Namespace && ref.Pod == self.Pod && ref.UID == self.UID {
			return nil
		}
		files = append(files, FileState{
			Path:    p,
			Ref:     ref,
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
			Inode:   inodeFromInfo(fi),
		})
		return nil
	})
	if err != nil {
		return Scan{}, err
	}

	type containerKey struct {
		namespace, pod, uid, container string
	}
	groups := make(map[containerKey][]FileState)
	for _, f := range files {
		key := containerKey{f.Ref.Namespace, f.Ref.Pod, f.Ref.UID, f.Ref.Container}
		groups[key] = append(groups[key], f)
	}

	var current []FileState
	var rotated []FileState

	for _, group := range groups {
		maxRestart := -1
		var maxFiles []FileState
		for _, f := range group {
			if !f.Ref.Rotated {
				if f.Ref.RestartCount > maxRestart {
					maxRestart = f.Ref.RestartCount
					maxFiles = []FileState{f}
				} else if f.Ref.RestartCount == maxRestart {
					maxFiles = append(maxFiles, f)
				}
			}
		}
		if maxRestart == -1 {
			rotated = append(rotated, group...)
			continue
		}
		currentFile := maxFiles[0]
		for _, f := range maxFiles[1:] {
			if f.ModTime.After(currentFile.ModTime) || (f.ModTime.Equal(currentFile.ModTime) && f.Path > currentFile.Path) {
				currentFile = f
			}
		}
		current = append(current, currentFile)
		for _, f := range group {
			if f.Path != currentFile.Path {
				rotated = append(rotated, f)
			}
		}
	}

	sort.Slice(current, func(i, j int) bool { return current[i].Path < current[j].Path })
	sort.Slice(rotated, func(i, j int) bool {
		ri, rj := rotated[i].Ref, rotated[j].Ref
		if ri.Namespace != rj.Namespace {
			return ri.Namespace < rj.Namespace
		}
		if ri.Pod != rj.Pod {
			return ri.Pod < rj.Pod
		}
		if ri.UID != rj.UID {
			return ri.UID < rj.UID
		}
		if ri.Container != rj.Container {
			return ri.Container < rj.Container
		}
		if !rotated[i].ModTime.Equal(rotated[j].ModTime) {
			return rotated[i].ModTime.Before(rotated[j].ModTime)
		}
		if ri.RestartCount != rj.RestartCount {
			return ri.RestartCount < rj.RestartCount
		}
		return rotated[i].Path < rotated[j].Path
	})

	return Scan{Files: current, Rotated: rotated}, nil
}

type fileKey struct {
	namespace, pod, uid, container string
	restart                        int
}

func refKey(r PodRef) fileKey {
	return fileKey{r.Namespace, r.Pod, r.UID, r.Container, r.RestartCount}
}

func Diff(prev, cur Scan) []Change {
	prevCurr := make(map[fileKey]FileState, len(prev.Files))
	for _, f := range prev.Files {
		prevCurr[refKey(f.Ref)] = f
	}
	curCurr := make(map[fileKey]FileState, len(cur.Files))
	for _, f := range cur.Files {
		curCurr[refKey(f.Ref)] = f
	}
	prevRotPaths := make(map[string]bool, len(prev.Rotated))
	for _, f := range prev.Rotated {
		prevRotPaths[f.Path] = true
	}

	var changes []Change

	for key, curFile := range curCurr {
		if _, ok := prevCurr[key]; !ok {
			changes = append(changes, Change{Kind: Added, After: curFile})
		}
	}

	for key, prevFile := range prevCurr {
		curFile, exists := curCurr[key]
		if !exists {
			hasRotated := false
			for _, rf := range cur.Rotated {
				if refKey(rf.Ref) == key && !prevRotPaths[rf.Path] {
					hasRotated = true
					break
				}
			}
			if !hasRotated {
				changes = append(changes, Change{Kind: Removed, Before: prevFile})
			}
			continue
		}

		if prevFile.Inode != 0 && curFile.Inode != 0 && prevFile.Inode != curFile.Inode {
			changes = append(changes, Change{Kind: Rotated, Before: prevFile, After: curFile})
			continue
		}

		if curFile.Size < prevFile.Size {
			foundRotated := false
			for _, rf := range cur.Rotated {
				if refKey(rf.Ref) == key && rf.Size == prevFile.Size && !prevRotPaths[rf.Path] {
					foundRotated = true
					break
				}
			}
			if foundRotated {
				changes = append(changes, Change{Kind: Rotated, Before: prevFile, After: curFile})
			} else {
				changes = append(changes, Change{Kind: Truncated, Before: prevFile, After: curFile})
			}
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		pi := changes[i].After.Path
		if pi == "" {
			pi = changes[i].Before.Path
		}
		pj := changes[j].After.Path
		if pj == "" {
			pj = changes[j].Before.Path
		}
		return pi < pj
	})

	return changes
}
