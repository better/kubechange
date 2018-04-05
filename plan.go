package main

import (
	"fmt"
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

//todo: filter out Jobs that are children of CronJobs
func pairObjectsByCriteria(srcObjects []runtime.Object, dstObjects []runtime.Object, criteria PairCriteria) []ObjectPair {
	pairs := make([]ObjectPair, 0, len(srcObjects))

	for i := range srcObjects {
		srcMetadata, srcLabels := getObjectMetadata(srcObjects[i])
		pair := ObjectPair{&srcObjects[i], nil}

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

func waitForObjectDeletion(object runtime.Object, clientset kubernetes.Interface) error {
	return wait.PollImmediate(time.Second, time.Second*60, func() (bool, error) {
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

func generatePlan(pairs []ObjectPair) []Step {
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

//todo: figure out how to test this with a mock clientset (kubernetes.Interface?)
//use something like https://github.com/GoogleCloudPlatform/skaffold/blob/21116842e65c0c7ace293352fad2b1f4adb5c9b2/pkg/skaffold/kubernetes/client.go
func executePlan(plan []Step, config PlanConfig) {
	clientset := config.kubeclient
	execute := config.execute
	for _, step := range plan {
		if step.action == "create" {
			src := *step.pair.src
			srcMetadata, _ := getObjectMetadata(src)
			switch srcType := src.(type) {
			case *batchv2alpha1.CronJob:
				fmt.Println(`Creating CronJob "` + srcMetadata.GetName() + `"`)

				if !execute {
					break
				}

				_, err := clientset.BatchV2alpha1().CronJobs(srcMetadata.GetNamespace()).Create(src.(*batchv2alpha1.CronJob))

				if err != nil {
					panic(err)
				}

			case *batchv1.Job:
				fmt.Println(`Creating Job "` + srcMetadata.GetName() + `"`)

				if !execute {
					break
				}

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
			propagationPolicy := metav1.DeletePropagationForeground
			//todo: use object metadata instead of type switch
			switch srcType := src.(type) {
			case *batchv1.Job:
				dstGVK := getObjectGroupVersionKind(dst)

				if dstGVK.Kind == "Job" {
					fmt.Println(`Replacing Job "` + dstMetadata.GetName() + `" with Job "` + srcMetadata.GetName() + `"`)

					if !execute {
						break
					}

					err := clientset.BatchV1().Jobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), &metav1.DeleteOptions{PropagationPolicy: &propagationPolicy})

					if err != nil {
						panic(err)
					}

					waitForObjectDeletion(dst, clientset)

					_, err = clientset.BatchV1().Jobs(srcMetadata.GetNamespace()).Create(src.(*batchv1.Job))

					if err != nil {
						panic(err)
					}
				} else if dstGVK.Kind == "CronJob" {
					fmt.Println(`Replacing CronJob "` + dstMetadata.GetName() + `" with Job "` + srcMetadata.GetName() + `"`)

					if !execute {
						break
					}

					//todo: delete current CronJob child Jobs
					err := clientset.BatchV2alpha1().CronJobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), &metav1.DeleteOptions{PropagationPolicy: &propagationPolicy})
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
					fmt.Println(`Replacing Job "` + dstMetadata.GetName() + `" with CronJob "` + srcMetadata.GetName() + `"`)

					if !execute {
						break
					}

					err := clientset.BatchV1().Jobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), &metav1.DeleteOptions{PropagationPolicy: &propagationPolicy})

					if err != nil {
						panic(err)
					}

					waitForObjectDeletion(dst, clientset)

					_, err = clientset.BatchV2alpha1().CronJobs(srcMetadata.GetNamespace()).Create(src.(*batchv2alpha1.CronJob))

					if err != nil {
						panic(err)
					}
				} else if dstGVK.Kind == "CronJob" {
					fmt.Println(`Replacing CronJob "` + dstMetadata.GetName() + `" with CronJob "` + srcMetadata.GetName() + `"`)

					if !execute {
						break
					}
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

	if len(plan) == 0 {
		fmt.Println("Nothing to do")
	} else {
		fmt.Println("Finished")
	}
}
