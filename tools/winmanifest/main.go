// Command winmanifest generates a Windows COFF resource object (.syso) that
// embeds an application manifest as an RT_MANIFEST resource.
//
// Why this exists: Windows has a UAC "Installer Detection" heuristic that, for
// any executable WITHOUT a declared execution level, inspects the file name and
// — if it contains words like "setup", "install", or "update" — assumes the
// program is an installer and forces an elevation (admin) prompt before the
// process even starts. Our binary is literally named scrub-setup.exe, so it trips
// the heuristic. Embedding a manifest that declares
// requestedExecutionLevel="asInvoker" both states the truth (the program needs no
// elevation) and disables the heuristic, because it only applies to executables
// that have no requestedExecutionLevel.
//
// How it is used: the Go linker automatically links a file named
// *_windows_amd64.syso sitting next to a main package into the windows/amd64
// build. We generate the .syso once and commit it, so normal builds and CI
// cross-compiles need no build-time tooling at all.
//
// This is pure standard library — no third-party dependencies. The COFF and
// resource-directory layout mirrors the well-known github.com/akavel/rsrc tool;
// every byte written here is explained inline so a reviewer can audit it.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
)

const (
	imageFileMachineAMD64 = 0x8664     // IMAGE_FILE_MACHINE_AMD64
	imageScnCntInitData   = 0x00000040 // IMAGE_SCN_CNT_INITIALIZED_DATA
	imageScnMemRead       = 0x40000000 // IMAGE_SCN_MEM_READ
	imageSymClassStatic   = 3          // IMAGE_SYM_CLASS_STATIC
	imageRelAMD64Addr32NB = 3          // IMAGE_REL_AMD64_ADDR32NB (RVA, no base)

	rtManifest = 24         // RT_MANIFEST resource type
	manifestID = 1          // CREATEPROCESS_MANIFEST_RESOURCE_ID
	langEnUS   = 1033       // resource language (en-US)
	subdirFlag = 0x80000000 // high bit of OffsetToData => points at a subdirectory
)

func main() {
	manifestPath := flag.String("manifest", "", "path to the .manifest XML file to embed (required)")
	outPath := flag.String("out", "rsrc_windows_amd64.syso", "output .syso path")
	flag.Parse()

	if *manifestPath == "" {
		fmt.Fprintln(os.Stderr, "winmanifest: -manifest is required")
		os.Exit(2)
	}
	manifest, err := os.ReadFile(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winmanifest: %v\n", err)
		os.Exit(1)
	}

	syso := buildSyso(manifest)
	if err := os.WriteFile(*outPath, syso, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "winmanifest: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("winmanifest: wrote %s (%d bytes) embedding %s (%d bytes)\n",
		*outPath, len(syso), *manifestPath, len(manifest))
}

func align(n, a int) int { return (n + a - 1) &^ (a - 1) }

// buildSyso assembles a COFF object file containing a single .rsrc section whose
// resource tree is root -> RT_MANIFEST -> id 1 -> language -> data entry -> bytes.
func buildSyso(manifest []byte) []byte {
	const (
		dirSize     = 16 // IMAGE_RESOURCE_DIRECTORY
		entrySize   = 8  // IMAGE_RESOURCE_DIRECTORY_ENTRY
		dataEntSize = 16 // IMAGE_RESOURCE_DATA_ENTRY
	)

	// Three nested directories, each with exactly one entry, then the data entry,
	// then the manifest bytes. Offsets are relative to the start of the section.
	dataEntryOff := 3 * (dirSize + entrySize) // 72
	manifestOff := dataEntryOff + dataEntSize // 88

	secLen := align(manifestOff+len(manifest), 8)
	sec := make([]byte, secLen)
	le := binary.LittleEndian

	// writeDir lays down a directory header advertising a single ID entry.
	writeDir := func(off int) {
		le.PutUint16(sec[off+12:], 0) // NumberOfNamedEntries
		le.PutUint16(sec[off+14:], 1) // NumberOfIdEntries
	}

	// Level 1: root, one entry keyed by resource type.
	writeDir(0)
	le.PutUint32(sec[16:], rtManifest)
	le.PutUint32(sec[20:], subdirFlag|24)

	// Level 2: the RT_MANIFEST type, one entry keyed by resource id.
	writeDir(24)
	le.PutUint32(sec[40:], manifestID)
	le.PutUint32(sec[44:], subdirFlag|48)

	// Level 3: the id, one entry keyed by language; this leaf points at the data
	// entry (high bit clear).
	writeDir(48)
	le.PutUint32(sec[64:], langEnUS)
	le.PutUint32(sec[68:], uint32(dataEntryOff))

	// The data entry. OffsetToData is an image RVA, fixed up by the relocation
	// below; we store the in-section offset of the bytes as the addend.
	le.PutUint32(sec[dataEntryOff+0:], uint32(manifestOff))   // OffsetToData
	le.PutUint32(sec[dataEntryOff+4:], uint32(len(manifest))) // Size
	le.PutUint32(sec[dataEntryOff+8:], 0)                     // CodePage
	le.PutUint32(sec[dataEntryOff+12:], 0)                    // Reserved
	copy(sec[manifestOff:], manifest)

	var buf bytes.Buffer
	w16 := func(v uint16) { _ = binary.Write(&buf, le, v) }
	w32 := func(v uint32) { _ = binary.Write(&buf, le, v) }

	ptrRawData := 20 + 40           // after the file header + one section header
	ptrReloc := ptrRawData + secLen // relocations follow the section data
	ptrSymtab := ptrReloc + 10      // one relocation is 10 bytes

	// COFF file header (20 bytes).
	w16(imageFileMachineAMD64) // Machine
	w16(1)                     // NumberOfSections
	w32(0)                     // TimeDateStamp
	w32(uint32(ptrSymtab))     // PointerToSymbolTable
	w32(1)                     // NumberOfSymbols
	w16(0)                     // SizeOfOptionalHeader
	w16(0)                     // Characteristics

	// Section header for ".rsrc" (40 bytes).
	buf.Write([]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0}) // Name[8]
	w32(0)                                              // VirtualSize
	w32(0)                                              // VirtualAddress
	w32(uint32(secLen))                                 // SizeOfRawData
	w32(uint32(ptrRawData))                             // PointerToRawData
	w32(uint32(ptrReloc))                               // PointerToRelocations
	w32(0)                                              // PointerToLinenumbers
	w16(1)                                              // NumberOfRelocations
	w16(0)                                              // NumberOfLinenumbers
	w32(imageScnCntInitData | imageScnMemRead)          // Characteristics

	// Section raw data.
	buf.Write(sec)

	// One relocation (10 bytes): make the data entry's OffsetToData section-relative.
	w32(uint32(dataEntryOff))  // VirtualAddress of the field to fix up
	w32(0)                     // SymbolTableIndex -> symbol 0 (the .rsrc section)
	w16(imageRelAMD64Addr32NB) // Type

	// Symbol table: one static symbol anchored at the start of the .rsrc section.
	buf.Write([]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0}) // Name[8]
	w32(0)                                              // Value
	_ = binary.Write(&buf, le, int16(1))                // SectionNumber
	w16(0)                                              // Type
	buf.WriteByte(imageSymClassStatic)                  // StorageClass
	buf.WriteByte(0)                                    // NumberOfAuxSymbols

	// String table: just its own size (4) — we use no long names.
	w32(4)

	return buf.Bytes()
}
