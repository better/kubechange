package main

import (
	"testing"
)

func TestParsing(t *testing.T) {
	files := readFiles([]string{"example-test-job.yml"})

	if len(files) != 1 {
		t.Errorf("Failed to read files")
	}

	for _, file := range files {
		objects, err := parseManifests(file)

		if err != nil {
			t.Errorf("Failed parsing manifests")
		}

		if len(objects) != 1 {
			t.Errorf("Failed to parse objects in manifest")
		}

		err = validateObjects(objects)

		if err != nil {
			t.Errorf("Failed validating objects")
		}
	}
}
