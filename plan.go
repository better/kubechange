package main

import (
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	batchv1 "k8s.io/client-go/pkg/apis/batch/v1"
	batchv2alpha1 "k8s.io/client-go/pkg/apis/batch/v2alpha1"
)

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

func getObjectNamespaces(objects []runtime.Object) []string {
	foundNamespaces := make(map[string]bool)

	for _, o := range objects {
		metadata, _ := getObjectMetadata(o)
		ns := metadata.GetNamespace()
		foundNamespaces[ns] = true
	}

	var namespaces []string

	for ns := range foundNamespaces {
		namespaces = append(namespaces, ns)
	}

	return namespaces
}

func waitForObjectDeletion(object runtime.Object, clientset *kubernetes.Clientset) error {
	return wait.PollImmediate(time.Millisecond*100, time.Second*60, func() (bool, error) {
		var err error
		switch t := object.(type) {
		case *batchv1.Job:
			metadata, _ := meta.Accessor(t)
			_, err = clientset.BatchV1().Jobs(metadata.GetNamespace()).Get(metadata.GetName(), metav1.GetOptions{})
		case *batchv2alpha1.CronJob:
			metadata, _ := meta.Accessor(t)
			_, err = clientset.BatchV2alpha1().CronJobs(metadata.GetNamespace()).Get(metadata.GetName(), metav1.GetOptions{})
		}

		if err == nil {
			return false, nil
		} else if errors.IsNotFound(err) {
			return true, nil
		} else {
			return false, err
		}
	})
}

func generatePlan(filenames []string, label *string, clientset *kubernetes.Clientset) []Step {
	files := readFiles(filenames)
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

	return plan
}

func executePlan(plan []Step, clientset *kubernetes.Clientset) {
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
				dstGVK := getObjectGroupVersionKind(dst)

				if dstGVK.Kind == "Job" {
					//todo: set propagation policy?
					err := clientset.BatchV1().Jobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), nil)

					if err != nil {
						panic(err)
					}

					waitForObjectDeletion(dst, clientset)

					_, err = clientset.BatchV1().Jobs(srcMetadata.GetNamespace()).Create(src.(*batchv1.Job))

					if err != nil {
						panic(err)
					}
				} else if dstGVK.Kind == "CronJob" {
					//todo: set propagation policy?
					//todo: delete current CronJob child Jobs
					err := clientset.BatchV2alpha1().CronJobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), nil)
					if err != nil {
						panic(err)
					}

					waitForObjectDeletion(dst, clientset)

					_, err = clientset.BatchV1().Jobs(srcMetadata.GetNamespace()).Create(src.(*batchv1.Job))

					if err != nil {
						panic(err)
					}
				}
			case *batchv2alpha1.CronJob:
				dstGVK := getObjectGroupVersionKind(dst)

				if dstGVK.Kind == "Job" {
					//todo: set propagation policy?
					err := clientset.BatchV1().Jobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), nil)

					if err != nil {
						panic(err)
					}

					waitForObjectDeletion(dst, clientset)

					_, err = clientset.BatchV2alpha1().CronJobs(srcMetadata.GetNamespace()).Create(src.(*batchv2alpha1.CronJob))

					if err != nil {
						panic(err)
					}
				} else if dstGVK.Kind == "CronJob" {
					_, err := clientset.BatchV2alpha1().CronJobs(srcMetadata.GetNamespace()).Update(src.(*batchv2alpha1.CronJob))

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
