package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apilabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1 "k8s.io/client-go/pkg/api/v1"
	batchv1 "k8s.io/client-go/pkg/apis/batch/v1"
	batchv2alpha1 "k8s.io/client-go/pkg/apis/batch/v2alpha1"
	"k8s.io/client-go/tools/clientcmd"
)

//todo: convert panic() calls to log errors in a structured way
//todo: should fail if more than one resource is matched on selector (for some resources?)
//todo: should also fail if the source resources don't match selector
//todo: get field names from json tags
//todo: consider comparing json (or deserialized json) instead of direct fields
//todo: consider implementing visitor pattern similar to kubectl for comparisons

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

func getObjectType(object runtime.Object) string {
	switch t := object.(type) {
	case *batchv1.Job:
		return "batchv1/Job"
	case *batchv2alpha1.CronJob:
		return "batchv2alpha1/CronJob"
	default:
		_ = t
		return "unknown"
	}
}

func validateObjects(objects []runtime.Object) error {
	for _, o := range objects {
		t := getObjectType(o)
		if t == "unknown" {
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

type Step struct {
	pair   ObjectPair
	action string
}

func pairObjectsByCriteria(srcObjects []runtime.Object, dstObjects []runtime.Object, criteria PairCriteria) []ObjectPair {
	pairs := make([]ObjectPair, 0, len(srcObjects))

	for _, src := range srcObjects {
		srcMetadata, srcLabels := getObjectMetadata(src)
		pair := ObjectPair{&src, nil}

		for _, dst := range dstObjects {
			dstMetadata, dstLabels := getObjectMetadata(dst)

			if criteria.label != "" &&
				srcLabels.Get(criteria.label) == dstLabels.Get(criteria.label) &&
				srcMetadata.GetNamespace() == dstMetadata.GetNamespace() {
				pair.dst = &dst
			}
		}

		pairs = append(pairs, pair)
	}

	//todo: iterate over dstObjects

	return pairs
}

func deepCompareObject(src runtime.Object, dst runtime.Object) []string {
	var fields []string

	if src == nil || dst == nil || getObjectType(src) != getObjectType(dst) {
		return []string{"kind"}
	}

	switch srcType := src.(type) {
	case *batchv1.Job:
		dstJob := dst.(*batchv1.Job)
		return deepCompareJobSpec(srcType.Spec, dstJob.Spec)
	case *batchv2alpha1.CronJob:
		dstCronJob := dst.(*batchv2alpha1.CronJob)
		return deepCompareCronJobSpec(srcType.Spec, dstCronJob.Spec)
	}

	return fields
}

func deepCompareCronJobSpec(src batchv2alpha1.CronJobSpec, dst batchv2alpha1.CronJobSpec) []string {
	var fields []string

	if src.Schedule != dst.Schedule {
		fields = append(fields, "schedule")
	}

	if src.ConcurrencyPolicy != dst.ConcurrencyPolicy {
		fields = append(fields, "schedule")
	}

	if src.Suspend != nil {
		if dst.Suspend == nil || *src.Suspend != *dst.Suspend {
			fields = append(fields, "suspend")
		}
	}

	if src.SuccessfulJobsHistoryLimit != nil {
		if dst.SuccessfulJobsHistoryLimit == nil || *src.SuccessfulJobsHistoryLimit != *dst.SuccessfulJobsHistoryLimit {
			fields = append(fields, "successfulJobsHistoryLimit")
		}
	}

	if src.FailedJobsHistoryLimit != nil {
		if dst.FailedJobsHistoryLimit == nil || *src.FailedJobsHistoryLimit != *dst.FailedJobsHistoryLimit {
			fields = append(fields, "failedJobsHistoryLimit")
		}
	}

	fields = append(fields, deepCompareJobTemplateSpec(src.JobTemplate, dst.JobTemplate)...)

	return fields
}

func deepCompareJobTemplateSpec(src batchv2alpha1.JobTemplateSpec, dst batchv2alpha1.JobTemplateSpec) []string {
	var fields []string

	fields = append(fields, deepCompareJobSpec(src.Spec, dst.Spec)...)

	return fields
}

func deepCompareJobSpec(src batchv1.JobSpec, dst batchv1.JobSpec) []string {
	var fields []string

	if src.ActiveDeadlineSeconds != nil {
		if dst.ActiveDeadlineSeconds == nil || *src.ActiveDeadlineSeconds != *dst.ActiveDeadlineSeconds {
			fields = append(fields, "activeDeadlineSeconds")
		}
	}

	fields = append(fields, deepComparePodTemplateSpec(src.Template, dst.Template)...)

	return fields
}

func deepComparePodTemplateSpec(src v1.PodTemplateSpec, dst v1.PodTemplateSpec) []string {
	var fields []string

	fields = append(fields, deepComparePodSpec(src.Spec, dst.Spec)...)

	return fields
}

func deepComparePodSpec(src v1.PodSpec, dst v1.PodSpec) []string {
	var fields []string

	if src.RestartPolicy != dst.RestartPolicy {
		fields = append(fields, "restartPolicy")
	}

	if src.TerminationGracePeriodSeconds != nil {
		if dst.TerminationGracePeriodSeconds == nil || *src.TerminationGracePeriodSeconds != *dst.TerminationGracePeriodSeconds {
			fields = append(fields, "terminationGracePeriodSeconds")
		}
	}

	if src.ActiveDeadlineSeconds != nil {
		if dst.ActiveDeadlineSeconds == nil || *src.ActiveDeadlineSeconds != *dst.ActiveDeadlineSeconds {
			fields = append(fields, "activeDeadlineSeconds")
		}
	}

	if len(compareNodeSelector(src.NodeSelector, dst.NodeSelector)) > 0 {
		fields = append(fields, "nodeSelector")
	}

	if compareContainerArray(src.Containers, dst.Containers) {
		fields = append(fields, "containers")
	}

	return fields
}

func compareNodeSelector(src map[string]string, dst map[string]string) []string {
	var fields []string

	for srcKey, srcVal := range src {
		diff := true
		if dstVal, ok := dst[srcKey]; ok {
			if srcVal == dstVal {
				diff = false
			}
		}

		if diff {
			fields = append(fields, srcKey)
		}
	}

	return fields
}

//todo: maybe refactor to return actually differing fields
func compareContainerArray(src []v1.Container, dst []v1.Container) bool {
	if len(src) != len(dst) {
		return true
	}

	for _, srcContainer := range src {
		for _, dstContainer := range dst {
			var foundMatchingContainer bool
			if dstContainer.Name == srcContainer.Name {
				foundMatchingContainer = true

				if srcContainer.Image != dstContainer.Image {
					return true
				} else if srcContainer.WorkingDir != dstContainer.WorkingDir {
					return true
				} else if strings.Join(srcContainer.Command, " ") != strings.Join(dstContainer.Command, " ") {
					return true
				} else if strings.Join(srcContainer.Args, " ") != strings.Join(dstContainer.Args, " ") {
					return true
				}

				srcEnv, _ := json.Marshal(srcContainer.Env)
				dstEnv, _ := json.Marshal(dstContainer.Env)

				if string(srcEnv) != string(dstEnv) {
					return true
				}
			}

			if foundMatchingContainer == false {
				return true
			}
		}
	}

	return false
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

		cronjobs, _ := clientset.BatchV2alpha1().CronJobs(ns).List(metav1.ListOptions{})
		for _, cronjob := range cronjobs.Items {
			o := runtime.Object(&cronjob)
			remoteObjects = append(remoteObjects, o)
		}
	}

	dstObjects := filterObjectsByLabel(remoteObjects, *label)
	pairs := pairObjectsByCriteria(srcObjects, dstObjects, PairCriteria{*label})
	plan := make([]Step, 0, 1)

	for _, pair := range pairs {
		var action string

		if pair.dst == nil {
			action = "create"
		} else if pair.src == nil {
			action = "delete"
		} else if pair.dst != nil {
			pairDiffFields := deepCompareObject(*pair.src, *pair.dst)
			if len(pairDiffFields) > 0 {
				action = "update"
			}
		}

		if action != "" {
			plan = append(plan, Step{pair: pair, action: action})
		}
	}

	for _, step := range plan {
		if step.action == "create" {
			src := *step.pair.src
			srcMetadata, _ := getObjectMetadata(src)
			switch srcType := src.(type) {
			case *batchv2alpha1.CronJob:
				_, err := clientset.BatchV2alpha1().CronJobs(srcMetadata.GetNamespace()).Create(src.(*batchv2alpha1.CronJob))

				if err != nil {
					panic(err)
				}

			case *batchv1.Job:
				_, err := clientset.BatchV1().Jobs(srcMetadata.GetNamespace()).Create(src.(*batchv1.Job))

				if err != nil {
					panic(err)
				}
			default:
				_ = srcType
			}
		} else if step.action == "update" {
			src := *step.pair.src
			dst := *step.pair.dst
			srcMetadata, _ := getObjectMetadata(src)
			dstMetadata, _ := getObjectMetadata(dst)
			//todo: use object metadata instead of type switch
			switch srcType := src.(type) {
			case *batchv1.Job:
				dstType := getObjectType(dst)

				if dstType == "batchv1/Job" {
					//todo: set propagation policy?
					err := clientset.BatchV1().Jobs(dstMetadata.GetNamespace()).Delete(srcMetadata.GetName(), nil)

					if err != nil {
						panic(err)
					}

					//todo: wait until deleted
					//todo: wait/retry if object is being deleted
					_, err = clientset.BatchV1().Jobs(srcMetadata.GetNamespace()).Create(src.(*batchv1.Job))

					if err != nil {
						panic(err)
					}
				} else if dstType == "batchv2alpha1/CronJob" {
					//todo: set propagation policy?
					err := clientset.BatchV2alpha1().CronJobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), nil)
					if err != nil {
						panic(err)
					}

					//todo: wait until deleted
					//todo: wait/retry if object is being deleted
					_, err = clientset.BatchV1().Jobs(srcMetadata.GetNamespace()).Create(src.(*batchv1.Job))

					if err != nil {
						panic(err)
					}
				}
			case *batchv2alpha1.CronJob:
				dstType := getObjectType(dst)

				if dstType == "batchv1/Job" {
					//todo: set propagation policy?
					err := clientset.BatchV1().Jobs(dstMetadata.GetNamespace()).Delete(srcMetadata.GetName(), nil)

					if err != nil {
						panic(err)
					}

					//todo: wait until deleted
					//todo: wait/retry if object is being deleted
					_, err = clientset.BatchV2alpha1().CronJobs(srcMetadata.GetNamespace()).Create(src.(*batchv2alpha1.CronJob))

					if err != nil {
						panic(err)
					}
				} else if dstType == "batchv2alpha1/CronJob" {
					_, err = clientset.BatchV2alpha1().CronJobs(srcMetadata.GetNamespace()).Update(src.(*batchv2alpha1.CronJob))

					if err != nil {
						panic(err)
					}
				}
			default:
				_ = srcType
			}
		}
	}
}
