package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func checkFiles(t *testing.T, d *LimitedDir, wanted []string) {
	files := d.List()
	if len(files) != len(wanted) {
		t.Fatalf("expected %d files, got %d, %v != %v", len(wanted), len(files),
			wanted, files)
	}
	for i, f := range wanted {
		if f != files[i] {
			t.Fatal("expected '%s' file, got '%s' at position %d, %v != %v",
				f, files[i], i, wanted, files)
		}
	}
}

func TestLimitedDir(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "gribouillis")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	writeFile := func(name string, size int) {
		data := make([]byte, size)
		path := filepath.Join(tmpDir, name)
		err := ioutil.WriteFile(path, data, 0644)
		if err != nil {
			t.Fatal(err)
		}
	}

	d, err := OpenLimitedDir(tmpDir, 5, 4)
	if err != nil {
		t.Fatal(err)
	}
	addFile := func(name string, size int) {
		writeFile(name, size)
		err := d.Add(name)
		if err != nil {
			t.Fatalf("could not add %d: %s", name, err)
		}
	}

	checkFiles(t, d, nil)

	// Test maxcount
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("%d-1", i)
		addFile(name, 1)
	}
	checkFiles(t, d, []string{"1-1", "2-1", "3-1", "4-1"})

	// Test maxsize
	addFile("5-2", 2)
	checkFiles(t, d, []string{"2-1", "3-1", "4-1", "5-2"})
	addFile("6-2", 2)
	checkFiles(t, d, []string{"4-1", "5-2", "6-2"})
	addFile("7-3", 3)
	checkFiles(t, d, []string{"6-2", "7-3"})
	addFile("8-4", 4)
	checkFiles(t, d, []string{"8-4"})
	addFile("9-5", 5)
	checkFiles(t, d, []string{"9-5"})
	addFile("10-6", 6)
	checkFiles(t, d, []string{})

	// Reopen and shrink
	writeFile("11-1", 1)
	writeFile("12-1", 1)
	writeFile("13-2", 2)
	writeFile("14-1", 1)
	writeFile("15-2", 2)
	d2, err := OpenLimitedDir(tmpDir, 5, 4)
	if err != nil {
		t.Fatal(err)
	}
	checkFiles(t, d2, []string{"13-2", "14-1", "15-2"})
}
