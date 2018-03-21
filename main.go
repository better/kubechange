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
	batchv2alpha1 "k8s.io/client-go/pkg/apis/batch/v2alpha1"
	"k8s.io/client-go/tools/clientcmd"
)

//todo: convert panic() calls to log errors in a structured way
//todo: should fail if more than one resource is matched on selector (for some resources?)
//todo: should also fail if the source resources don't match selector

type PairCriteria struct {
	label string
}

type ObjectPair struct {
	src *runtime.Object
	dst *runtime.Object
}

type Step struct {
	pair   ObjectPair
	action string
}

//need to move clientset to a struct because clientset type checks fail when using fake clientset as argument
type PlanConfig struct {
	kubeclient kubernetes.Interface
	execute bool
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

func getObjectGroupVersionKind(object runtime.Object) schema.GroupVersionKind {
	switch t := object.(type) {
	case *batchv1.Job:
		return schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}
	case *batchv2alpha1.CronJob:
		return schema.GroupVersionKind{Group: "batch", Version: "v2alpha1", Kind: "CronJob"}
	default:
		_ = t
		return schema.GroupVersionKind{}
	}
}

func validateObjects(objects []runtime.Object) error {
	for _, o := range objects {
		gvk := getObjectGroupVersionKind(o)
		if gvk.Group == "" {
			return errors.New("Not an accepted resource")
		}
	}
	return nil
}

func getObjectMetadata(o runtime.Object) (metav1.Object, apilabels.Set) {
	switch t := o.(type) {
	default:
		metadata, err := meta.Accessor(t)

		if err != nil {
			panic(err)
		}

		return metadata, apilabels.Set(metadata.GetLabels())
	}
}

func main() {
	label := flag.String("l", "", "Label to use as filter")
	execute := flag.Bool("e", false, "Update cluster objects")

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

	filenames := flag.Args()

	if len(filenames) == 0 {
		flag.Usage()
		return
	}

	if *label == "" {
		panic(errors.New("Missing label"))
	}

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)

	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)

	if err != nil {
		panic(err.Error())
	}

	files := readFiles(filenames)
	localObjects := make([]runtime.Object, 0, 1)

	for _, file := range files {
		o, err := parseManifests(file)
		if err != nil {
			panic(err)
		}
		localObjects = append(localObjects, o...)
	}

	err = validateObjects(localObjects)

	if err != nil {
		panic(err)
	}

	srcObjects := filterObjectsByLabel(localObjects, *label)
	//todo: consider all namespaces
	namespaces := getObjectNamespaces(srcObjects)

	remoteObjects := make([]runtime.Object, 0, 1)

	for _, ns := range namespaces {
		jobs, _ := clientset.BatchV1().Jobs(ns).List(metav1.ListOptions{})
		for _, job := range jobs.Items {
			o := runtime.Object(&job)
			remoteObjects = append(remoteObjects, o)
		}

		cronjobs, _ := clientset.BatchV2alpha1().CronJobs(ns).List(metav1.ListOptions{})
		for _, cronjob := range cronjobs.Items {
			o := runtime.Object(&cronjob)
			remoteObjects = append(remoteObjects, o)
		}
	}

	dstObjects := filterObjectsByLabel(remoteObjects, *label)

	pairs := pairObjectsByCriteria(srcObjects, dstObjects, PairCriteria{*label})

	plan := generatePlan(pairs)

	if *execute != true {
		fmt.Printf("This is a preview. Run kubechange with -e to make cluster updates.\n\n")
	}

	executePlan(plan, PlanConfig{clientset, *execute})
}
