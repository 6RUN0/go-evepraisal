package staticdump

import (
	"archive/zip"
	"bufio"
	"fmt"
)

// maxJSONLineBytes bounds one JSONL line; the longest line in SDE build 3687575 is about 45 KB.
const maxJSONLineBytes = 1 << 20

func findZipFile(files []*zip.File, filename string) (*zip.File, error) {
	for _, f := range files {
		if filename == f.Name {
			return f, nil
		}
	}
	return nil, fmt.Errorf("could not locate %s in archive", filename)
}

// forEachJSONLine calls fn for every non-empty line of filename in the archive.
// Empty lines are counted in lineNo but not passed to fn; a line over 1 MiB is an error.
func forEachJSONLine(r *zip.ReadCloser, filename string, fn func(lineNo int, line []byte) error) error {
	f, err := findZipFile(r.File, filename)
	if err != nil {
		return err
	}
	fr, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", filename, err)
	}
	defer fr.Close()

	scanner := bufio.NewScanner(fr)
	scanner.Buffer(make([]byte, 0, 64*1024), maxJSONLineBytes)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := fn(lineNo, line); err != nil {
			return fmt.Errorf("%s line %d: %w", filename, lineNo, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%s line %d: %w", filename, lineNo+1, err)
	}
	return nil
}
