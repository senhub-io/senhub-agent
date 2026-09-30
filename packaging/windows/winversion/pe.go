package main

import (
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

const (
	rtVersion          = 16
	fixedFileInfoMagic = 0xFEEF04BD
)

var errNoVersionResource = errors.New("no VERSIONINFO resource")

type versionInfo struct {
	FileVersion    [4]uint16
	ProductVersion [4]uint16
	Strings        map[string]string
}

// readVersionInfo extracts the VERSIONINFO resource of a PE file: the
// numeric versions of VS_FIXEDFILEINFO and every string of the string
// tables. It returns errNoVersionResource when the file has none, which is
// what an exe built without the .syso looks like.
func readVersionInfo(path string) (*versionInfo, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	sec := f.Section(".rsrc")
	if sec == nil {
		return nil, errNoVersionResource
	}
	rsrc, err := sec.Data()
	if err != nil {
		return nil, fmt.Errorf("reading .rsrc of %s: %w", path, err)
	}
	data, err := findVersionResource(rsrc, sec.VirtualAddress)
	if err != nil {
		return nil, err
	}
	return parseVersionInfo(data)
}

// findVersionResource walks the three-level resource directory (type,
// name, language) down to the first RT_VERSION leaf.
func findVersionResource(rsrc []byte, sectionRVA uint32) ([]byte, error) {
	entry, found, err := dirEntry(rsrc, 0, func(id uint32) bool { return id == rtVersion })
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errNoVersionResource
	}
	for level := 0; level < 2; level++ {
		if entry&0x80000000 == 0 {
			return nil, fmt.Errorf("resource tree: expected a directory at level %d", level+1)
		}
		entry, found, err = dirEntry(rsrc, entry&0x7FFFFFFF, func(uint32) bool { return true })
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errNoVersionResource
		}
	}
	if entry&0x80000000 != 0 {
		return nil, fmt.Errorf("resource tree: expected a data entry for RT_VERSION")
	}
	if int(entry)+16 > len(rsrc) {
		return nil, fmt.Errorf("resource tree: data entry out of bounds")
	}
	rva := binary.LittleEndian.Uint32(rsrc[entry:])
	size := binary.LittleEndian.Uint32(rsrc[entry+4:])
	start := int64(rva) - int64(sectionRVA)
	if start < 0 || start+int64(size) > int64(len(rsrc)) {
		return nil, fmt.Errorf("resource tree: RT_VERSION data out of the .rsrc section")
	}
	return rsrc[start : start+int64(size)], nil
}

// dirEntry returns the offset-or-data field of the first entry of the
// IMAGE_RESOURCE_DIRECTORY at off whose integer ID satisfies match.
func dirEntry(rsrc []byte, off uint32, match func(uint32) bool) (uint32, bool, error) {
	if int(off)+16 > len(rsrc) {
		return 0, false, fmt.Errorf("resource directory at %#x out of bounds", off)
	}
	named := int(binary.LittleEndian.Uint16(rsrc[off+12:]))
	ids := int(binary.LittleEndian.Uint16(rsrc[off+14:]))
	for i := 0; i < named+ids; i++ {
		e := int(off) + 16 + i*8
		if e+8 > len(rsrc) {
			return 0, false, fmt.Errorf("resource directory entry out of bounds")
		}
		name := binary.LittleEndian.Uint32(rsrc[e:])
		if name&0x80000000 != 0 {
			continue
		}
		if match(name) {
			return binary.LittleEndian.Uint32(rsrc[e+4:]), true, nil
		}
	}
	return 0, false, nil
}

// node is one VS_VERSIONINFO-style block: wLength, wValueLength, wType,
// a NUL-terminated UTF-16 key, a value, then child blocks, each aligned
// on 32 bits.
type node struct {
	key      string
	value    []byte
	children []node
}

func parseNode(b []byte) (node, int, error) {
	if len(b) < 6 {
		return node{}, 0, fmt.Errorf("version block truncated")
	}
	length := int(binary.LittleEndian.Uint16(b))
	valueLen := int(binary.LittleEndian.Uint16(b[2:]))
	isText := binary.LittleEndian.Uint16(b[4:]) == 1
	if length < 6 || length > len(b) {
		return node{}, 0, fmt.Errorf("version block length %d invalid", length)
	}
	b = b[:length]
	pos := 6
	var key []uint16
	for {
		if pos+2 > len(b) {
			return node{}, 0, fmt.Errorf("version block key not terminated")
		}
		c := binary.LittleEndian.Uint16(b[pos:])
		pos += 2
		if c == 0 {
			break
		}
		key = append(key, c)
	}
	pos = align4(pos)
	n := node{key: string(utf16.Decode(key))}
	if isText {
		valueLen *= 2
	}
	if valueLen > 0 {
		if pos+valueLen > len(b) {
			return node{}, 0, fmt.Errorf("version block %q value out of bounds", n.key)
		}
		n.value = b[pos : pos+valueLen]
		pos = align4(pos + valueLen)
	}
	for pos < len(b) {
		child, used, err := parseNode(b[pos:])
		if err != nil {
			return node{}, 0, err
		}
		n.children = append(n.children, child)
		pos = align4(pos + used)
	}
	return n, length, nil
}

func align4(n int) int { return (n + 3) &^ 3 }

func parseVersionInfo(data []byte) (*versionInfo, error) {
	root, _, err := parseNode(data)
	if err != nil {
		return nil, err
	}
	if root.key != "VS_VERSION_INFO" {
		return nil, fmt.Errorf("RT_VERSION root key is %q, want VS_VERSION_INFO", root.key)
	}
	if len(root.value) < 52 || binary.LittleEndian.Uint32(root.value) != fixedFileInfoMagic {
		return nil, fmt.Errorf("VS_FIXEDFILEINFO missing or malformed")
	}
	v := root.value
	vi := &versionInfo{
		FileVersion:    splitVersion(binary.LittleEndian.Uint32(v[8:]), binary.LittleEndian.Uint32(v[12:])),
		ProductVersion: splitVersion(binary.LittleEndian.Uint32(v[16:]), binary.LittleEndian.Uint32(v[20:])),
		Strings:        map[string]string{},
	}
	for _, sfi := range root.children {
		if sfi.key != "StringFileInfo" {
			continue
		}
		for _, table := range sfi.children {
			for _, s := range table.children {
				vi.Strings[s.key] = decodeUTF16Z(s.value)
			}
		}
	}
	return vi, nil
}

func splitVersion(ms, ls uint32) [4]uint16 {
	return [4]uint16{uint16(ms >> 16), uint16(ms), uint16(ls >> 16), uint16(ls)}
}

func decodeUTF16Z(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		c := binary.LittleEndian.Uint16(b[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}
