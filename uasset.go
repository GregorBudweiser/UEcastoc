package uecastoc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tenfyzhong/cityhash"
)

// The ucas file consists of .uasset files with a structure that differs from the original .uasset structure.
// it is similar, but not exactly the same.

// static header with the first 64 bytes of the .uasset file
type UAssetHeader struct {
	RepeatNumber             [2]uint64 // same number repeated twice
	PackageFlags             uint32    // value will be 0x80000000
	TotalHeaderSize          uint32    // of the original file header size; but that included the dependencies...
	NamesDirectoryOffset     uint32    // points to nullbyte, so do +1
	NamesDirectoryLength     uint32    // length is in bytes
	NamesHashesOffset        uint32    // first entry is always the algorithm ID
	NamesHashesLength        uint32    // length is in bytes
	ImportObjectsOffset      uint32
	ExportObjectsOffset      uint32 // offset to some extra header information
	ExportMetaOffset         uint32 // points to some memory, just store the either 24 or 32 bytes.
	DependencyPackagesOffset uint32 // first value is a uint32, number of dependency packages.
	DependencyPackagesSize   uint64
}

// dependency packages will probably not change during modding
// type DependencyPackage struct {
// 	ID              uint64
// 	NumberOfEntries uint32 // not sure what this does exactly
// 	IsPresent       int32
// 	SomeValue       uint32 // mostly one, but sometimes 2
// 	// depending on the NumberOfEntries, there are multiple entries here
// 	ExtraEntries []uint32
// }

// total bytes: 72
type ExportObject struct {
	SerialOffset     uint64
	SerialSize       uint64
	ObjectNameOffset uint64 // index in the Names Directory list; name of this "file"
	ClassNameOffset  uint64
	OtherProperties  [40]byte
}
type UAssetResource struct {
	Header        UAssetHeader
	NamesDir      []string
	ExportObjects []ExportObject
	// the rest of the file probably won't change in mods
	ImportObjects      []byte
	ExportMeta         []byte
	DependencyPackages []byte
}

func (u *UAssetResource) PrintNamesDirectory() {
	for i, v := range u.NamesDir {
		fmt.Printf("[%02d][0x%02x]: %s\n", i, i, v)
	}
}
func parseNamesDirectory(namesBuffer *[]byte) *[]string {
	var strlist []string
	buff := (*namesBuffer)
	for len(buff) != 0 {
		strlen := uint8(buff[0])
		name := string(buff[1 : 1+strlen])
		strlist = append(strlist, name)
		buff = buff[strlen+2:]
	}
	return &strlist
}
func parseExportObjects(exportObjectBuff *[]byte) *[]ExportObject {
	buff := *exportObjectBuff
	var exports []ExportObject
	var export ExportObject
	// first, find the number of entries
	numberOfEntries := len(buff) / binary.Size(export)

	for i := 0; i < int(numberOfEntries); i++ {
		binary.Read(bytes.NewReader(buff), binary.LittleEndian, &export)
		exports = append(exports, export)
		buff = buff[binary.Size(export):]
	}
	return &exports
}

// hashes using CityHash64
func hashString(s *string) (hash uint64) {
	return cityhash.CityHash64([]byte(strings.ToLower(*s)))
}

// ParseUAssetFile takes a string and it expects the full .uasset file, including .uexp
// If the .uexp isn't there, there might be problems.
// Not all data must be parsed; only the parts that may be changed due to modding.
// That's what I'm doing here.
func ParseUAssetFile(path string) (uasset UAssetResource, err error) {
	f, err := os.Open(path)
	if err != nil {
		return
	}

	// parse header
	err = binary.Read(f, binary.LittleEndian, &uasset.Header)
	if err != nil {
		return
	}

	// parse names directory
	namesBuffer := make([]byte, uasset.Header.NamesDirectoryLength)
	f.Seek(int64(uasset.Header.NamesDirectoryOffset+1), io.SeekStart)
	n, err := f.Read(namesBuffer)
	if n != len(namesBuffer) || err != nil {
		return
	}
	uasset.NamesDir = *parseNamesDirectory(&namesBuffer)

	// hashes of names can be calculated; parsing not strictly required

	// import objects are unlikely to change; simply copy slice of bytes
	uasset.ImportObjects = make([]byte, uasset.Header.ExportObjectsOffset-uasset.Header.ImportObjectsOffset)
	f.Seek(int64(uasset.Header.ImportObjectsOffset), io.SeekStart)
	_, err = f.Read(uasset.ImportObjects)
	if err != nil {
		return
	}

	exportLen := uasset.Header.ExportMetaOffset - uasset.Header.ExportObjectsOffset
	f.Seek(int64(uasset.Header.ExportObjectsOffset), io.SeekStart)

	exportObjectsBuff := make([]byte, exportLen)
	_, err = f.Read(exportObjectsBuff)
	if err != nil {
		return
	}
	uasset.ExportObjects = *parseExportObjects(&exportObjectsBuff)

	// don't know how to change the kind of metadata, just copy
	uasset.ExportMeta = make([]byte, uasset.Header.DependencyPackagesOffset-uasset.Header.ExportMetaOffset)
	f.Seek(int64(uasset.Header.ExportMetaOffset), io.SeekStart)
	_, err = f.Read(uasset.ExportMeta)
	if err != nil {
		return
	}

	// these are unlikely to change; copy
	uasset.DependencyPackages = make([]byte, uasset.Header.DependencyPackagesSize)
	f.Seek(int64(uasset.Header.DependencyPackagesOffset), io.SeekStart)
	_, err = f.Read(uasset.DependencyPackages)
	if err != nil {
		return
	}

	// the actual data starts after this area
	uexpdataOffset := uint64(uasset.Header.DependencyPackagesOffset) + uasset.Header.DependencyPackagesSize
	offsetCorrection := uint64(0)
	if len(uasset.ExportObjects) > 0 {
		offsetCorrection = uasset.ExportObjects[0].SerialOffset - uexpdataOffset
	}
	for _, v := range uasset.ExportObjects {
		fmt.Printf("range %d - %d\n", v.SerialOffset-offsetCorrection, v.SerialOffset+v.SerialSize-offsetCorrection)
	}

	f.Seek(int64(uexpdataOffset), io.SeekStart)
	finfo, _ := f.Stat()
	uexpData := make([]byte, uint64(finfo.Size())-uexpdataOffset)

	_, err = f.Read(uexpData)
	if err != nil {
		return
	}
	f.Close()
	v := uasset.ParseUexp(&uexpData)

	jsonbytes, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(jsonbytes))
	fmt.Println("Length of this list:", len(v))
	// uasset.PrintNamesDirectory()
	return

}
