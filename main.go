package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apilabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	batchv1 "k8s.io/client-go/pkg/apis/batch/v1"
	"k8s.io/client-go/tools/clientcmd"
)

//todo: get remote jobs by label
// --
//todo: clean up JobSpec, removing any non-comparable fields
//todo: compare clean JobSpecs, detect difference
//todo: generate plan to deal with difference: create, update, delete
//todo: perform plan, confirm plan has succeeded
//todo: convert panic() calls to log errors in a structured way

/*
should fail if more than one resource is matched on selector (for some resources?)
should also fail if the source resources don't match selector
job cronjob parents can be looked up by metadata.ownerReferences (use uid)
*/

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

func parseManifests(file string) ([]runtime.Object, error) {
	objects := make([]runtime.Object, 0, 1)
	documents := strings.Split(file, "---")
	for _, m := range documents {
		if len(m) == 0 {
			continue
		}
		obj, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(file), nil, nil)

		if err != nil {
			return nil, err
		}

		objects = append(objects, obj)
	}

	return objects, nil
}

func validateObjects(objects []runtime.Object) error {
	for _, o := range objects {
		gvk := o.GetObjectKind().GroupVersionKind()
		if !isAcceptedGroupVersionKind(gvk) {
			return errors.New("Not an accepted resource: " + gvk.String())
		}
	}
	return nil
}

func getObjectMetadata(o runtime.Object) (metav1.Object, apilabels.Set) {
	switch t := o.(type) {
	case *batchv1.Job:
		metadata, err := meta.Accessor(t)

		if err != nil {
			panic(err)
		}

		return metadata, apilabels.Set(metadata.GetLabels())
	}

	return nil, nil
}

func filterObjectsByLabel(objects []runtime.Object, label string) []runtime.Object {
	filteredObjects := make([]runtime.Object, 0, 1)

	for _, o := range objects {
		_, labels := getObjectMetadata(o)
		if labels.Has(label) {
			filteredObjects = append(filteredObjects, o)
			continue
		}
	}

	return filteredObjects
}

type PairCriteria struct {
	label string
}

type ObjectPair struct {
	src *runtime.Object
	dst *runtime.Object
}

func pairObjectsByCriteria(srcObjects []runtime.Object, dstObjects []runtime.Object, criteria PairCriteria) []ObjectPair {
	pairs := make([]ObjectPair, 0, len(srcObjects))

	for _, src := range srcObjects {
		srcMetadata, srcLabels := getObjectMetadata(src)

		for _, dst := range dstObjects {
			dstMetadata, dstLabels := getObjectMetadata(dst)

			if criteria.label != "" &&
				srcLabels.Get(criteria.label) == dstLabels.Get(criteria.label) &&
				srcMetadata.GetNamespace() == dstMetadata.GetNamespace() {
				pairs = append(pairs, ObjectPair{&src, &dst})
			}
		}
	}

	return pairs
}

func getObjectNamespaces(objects []runtime.Object) []string {
	foundNamespaces := make(map[string]bool)

	for _, o := range objects {
		metadata, _ := getObjectMetadata(o)
		ns := metadata.GetNamespace()
		foundNamespaces[ns] = true
	}

	var namespaces []string

	for ns, _ := range foundNamespaces {
		namespaces = append(namespaces, ns)
	}

	return namespaces
}

func main() {
	label := flag.String("l", "", "Label to filter on")

	homedir := os.Getenv("HOME")

	if homedir == "" {
		homedir = os.Getenv("USERPROFILE")
	}

	var kubeconfig *string

	if homedir != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(homedir, ".kube", "config"), "(optional) Absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "Absolute path to the kubeconfig file")
	}

	flag.Parse()

	args := flag.Args()

	if len(args) == 0 {
		flag.Usage()
		return
	}

	if *label == "" {
		panic(errors.New("Missing label"))
	}

	files := readFiles(args)
	objects := make([]runtime.Object, 0, 1)

	for _, file := range files {
		o, err := parseManifests(file)
		if err != nil {
			panic(err)
		}
		objects = append(objects, o...)
	}

	err := validateObjects(objects)

	if err != nil {
		panic(err)
	}

	srcObjects := filterObjectsByLabel(objects, *label)
	srcNamespaces := getObjectNamespaces(srcObjects)

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)

	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)

	if err != nil {
		panic(err.Error())
	}

	remoteObjects := make([]runtime.Object, 0, 1)

	for _, ns := range srcNamespaces {
		jobs, _ := clientset.BatchV1().Jobs(ns).List(metav1.ListOptions{})
		for _, job := range jobs.Items {
			o := runtime.Object(&job)
			remoteObjects = append(remoteObjects, o)
		}
	}

	dstObjects := filterObjectsByLabel(remoteObjects, *label)
	pairs := pairObjectsByCriteria(srcObjects, dstObjects, PairCriteria{*label})

	for _, pair := range pairs {
		fmt.Println(*pair.src)
		fmt.Println(*pair.dst)
	}
}
