//go:build ignore

// Generate writes the desktop's multi-size ICO status marks when invoked by
// go generate from internal/desktop. It is excluded from ordinary package builds.
// These are original geometric bridge/status marks; no external artwork is used.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	colors := map[string][3]byte{
		"waiting": {210, 146, 18}, "linked": {28, 112, 190},
		"attention": {196, 44, 44}, "pending": {111, 78, 162},
	}
	for name, color := range colors {
		sizes := []int{16, 20, 24, 32, 40, 48, 64}
		var data bytes.Buffer
		binary.Write(&data, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
		offset := 6 + 16*len(sizes)
		var images [][]byte
		for _, size := range sizes {
			image := dib(size, color, name)
			data.Write([]byte{byte(size), byte(size), 0, 0})
			binary.Write(&data, binary.LittleEndian, uint16(1))
			binary.Write(&data, binary.LittleEndian, uint16(32))
			binary.Write(&data, binary.LittleEndian, uint32(len(image)))
			binary.Write(&data, binary.LittleEndian, uint32(offset))
			offset += len(image)
			images = append(images, image)
		}
		for _, image := range images {
			data.Write(image)
		}
		if err := os.WriteFile(filepath.Join("icons", name+".ico"), data.Bytes(), 0644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func dib(size int, color [3]byte, state string) []byte {
	maskStride := ((size + 31) / 32) * 4
	image := make([]byte, 40+size*size*4+maskStride*size)
	binary.LittleEndian.PutUint32(image, 40)
	binary.LittleEndian.PutUint32(image[4:], uint32(size))
	binary.LittleEndian.PutUint32(image[8:], uint32(size*2))
	binary.LittleEndian.PutUint16(image[12:], 1)
	binary.LittleEndian.PutUint16(image[14:], 32)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			nx, ny := float64(x)/float64(size), float64(y)/float64(size)
			dx, dy := nx-.5, ny-.5
			inside := dx*dx+dy*dy < .22
			white := false
			switch state {
			case "attention":
				white = nx > .44 && nx < .56 && ((ny > .22 && ny < .58) || (ny > .68 && ny < .8))
			case "pending":
				white = ny > .27 && ny < .73 && ((nx > .33 && nx < .44) || (nx > .56 && nx < .67))
			case "waiting":
				white = ny > .44 && ny < .56 && nx > .25 && nx < .75
			case "linked":
				white = (nx > .25 && nx < .38 && ny > .3 && ny < .7) ||
					(nx > .62 && nx < .75 && ny > .3 && ny < .7) ||
					(ny > .44 && ny < .56 && nx > .25 && nx < .75)
			}
			offset := 40 + ((size-1-y)*size+x)*4
			if inside {
				c := color
				if white {
					c = [3]byte{255, 255, 255}
				}
				copy(image[offset:offset+4], []byte{c[2], c[1], c[0], 255})
			} else {
				mask := 40 + size*size*4 + (size-1-y)*maskStride + x/8
				image[mask] |= 1 << (7 - x%8)
			}
		}
	}
	return image
}
