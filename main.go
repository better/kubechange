package main

//todo: convert panic() calls to log errors in a structured way

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
)

/*
should fail if more than one resource is matched on selector (for some resources?)
should also fail if the source resources don't match selector
job cronjob parents can be looked up by metadata.ownerReferences (use uid)
deployed metadata can be filtered out from comparisons
*/
type Manifest struct {
	Object           runtime.Object
	GroupVersionKind schema.GroupVersionKind
}

func isAcceptedGroupVersionKind(gvk schema.GroupVersionKind) bool {
	acceptedGroupVersionKinds := []schema.GroupVersionKind{
		{
			Group:   "batch",
			Version: "v1",
			Kind:    "Job",
		},
		{
			Group:   "batch",
			Version: "v2alpha1",
			Kind:    "CronJob",
		},
	}

	for _, k := range acceptedGroupVersionKinds {
		if gvk.Group == k.Group && gvk.Version == k.Version && gvk.Kind == k.Kind {
			return true
		}
	}

	return false
}

func readFiles(args []string) []string {
	var files = make([]string, 0, 1)

	if args[0] == "-" {
		b, _ := ioutil.ReadAll(os.Stdin)
		files = append(files, string(b))
	} else {
		for _, f := range args {
			b, err := ioutil.ReadFile(f)

			if err != nil {
				panic(err)
			}

			files = append(files, string(b))
		}
	}

	return files
}

func parseManifests(file string) ([]Manifest, error) {
	manifests := make([]Manifest, 0, 1)
	documents := strings.Split(file, "---")
	for _, m := range documents {
		if len(m) == 0 {
			continue
		}
		obj, groupVersionKind, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(file), nil, nil)

		if err != nil {
			return nil, err
		}

		manifests = append(manifests, Manifest{obj, *groupVersionKind})
	}

	return manifests, nil
}

func validateManifests(manifests []Manifest) error {
	for _, m := range manifests {
		if !isAcceptedGroupVersionKind(m.GroupVersionKind) {
			return errors.New("Not an accepted resource: " + m.GroupVersionKind.String())
		}
	}
	return nil
}

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) == 0 {
		flag.Usage()
		return
	}

	files := readFiles(args)
	manifests := make([]Manifest, 0, 1)

	for _, file := range files {
		m, err := parseManifests(file)
		if err != nil {
			panic(err)
		}
		manifests = append(manifests, m...)
	}

	err := validateManifests(manifests)

	if err != nil {
		panic(err)
	}

	fmt.Println(manifests)
}
