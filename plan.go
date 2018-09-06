package main

import (
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

//todo: filter out Jobs that are children of CronJobs
func pairObjectsByCriteria(srcObjects []runtime.Object, dstObjects []runtime.Object, criteria PairCriteria) []ObjectPair {
	pairs := make([]ObjectPair, 0, len(srcObjects))

	for i := range srcObjects {
		srcMetadata, srcLabels := getObjectMetadata(srcObjects[i])
		pair := ObjectPair{&srcObjects[i], nil}

		for j := range dstObjects {
			dstMetadata, dstLabels := getObjectMetadata(dstObjects[j])

			if criteria.label != "" &&
				srcLabels.Get(criteria.label) == dstLabels.Get(criteria.label) &&
				srcMetadata.GetNamespace() == dstMetadata.GetNamespace() {
				pair.dst = &dstObjects[j]
			}
		}
		pairs = append(pairs, pair)
	}

	for i := range dstObjects {
		dstMetadata, dstLabels := getObjectMetadata(dstObjects[i])
		pair := ObjectPair{nil, &dstObjects[i]}

		for j := range srcObjects {
			srcMetadata, srcLabels := getObjectMetadata(srcObjects[j])

			if criteria.label != "" &&
				srcLabels.Get(criteria.label) == dstLabels.Get(criteria.label) &&
				srcMetadata.GetNamespace() == dstMetadata.GetNamespace() {
				pair.src = &srcObjects[j]
			}
		}

		if pair.src == nil {
			pairs = append(pairs, pair)
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
		case *batchv1beta1.CronJob:
			metadata, _ := meta.Accessor(t)
			_, err = clientset.BatchV1beta1().CronJobs(metadata.GetNamespace()).Get(metadata.GetName(), metav1.GetOptions{})
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
			case *batchv1beta1.CronJob:
				fmt.Println(`Creating CronJob "` + srcMetadata.GetName() + `"`)

				if !execute {
					break
				}

				_, err := clientset.BatchV1beta1().CronJobs(srcMetadata.GetNamespace()).Create(src.(*batchv1beta1.CronJob))

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
		} else if step.action == "delete" {
			dst := *step.pair.dst
			dstMetadata, _ := getObjectMetadata(dst)
			propagationPolicy := metav1.DeletePropagationForeground

			switch dstType := dst.(type) {
			case *batchv1.Job:
				fmt.Println(`Deleting Job "` + dstMetadata.GetName() + `"`)

				if !execute {
					break
				}

				err := clientset.BatchV1().Jobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), &metav1.DeleteOptions{PropagationPolicy: &propagationPolicy})

				if err != nil {
					panic(err)
				}

				waitForObjectDeletion(dst, clientset)
			case *batchv1beta1.CronJob:
				fmt.Println(`Deleting CronJob "` + dstMetadata.GetName() + `"`)

				if !execute {
					break
				}

				err := clientset.BatchV1beta1().CronJobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), &metav1.DeleteOptions{PropagationPolicy: &propagationPolicy})

				if err != nil {
					panic(err)
				}

				waitForObjectDeletion(dst, clientset)
			default:
				_ = dstType
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
					fmt.Println(`Replacing Job "` + dstMetadata.GetName() + `" with Job "` + srcMetadata.GetName() + `" in ` + dstMetadata.GetNamespace() + ` namespace`)

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
					fmt.Println(`Replacing CronJob "` + dstMetadata.GetName() + `" with Job "` + srcMetadata.GetName() + `" in ` + dstMetadata.GetNamespace() + ` namespace`)

					if !execute {
						break
					}

					//todo: delete current CronJob child Jobs
					err := clientset.BatchV1beta1().CronJobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), &metav1.DeleteOptions{PropagationPolicy: &propagationPolicy})
					if err != nil {
						panic(err)
					}

					waitForObjectDeletion(dst, clientset)

					_, err = clientset.BatchV1().Jobs(srcMetadata.GetNamespace()).Create(src.(*batchv1.Job))

					if err != nil {
						panic(err)
					}
				}
			case *batchv1beta1.CronJob:
				dstGVK := getObjectGroupVersionKind(dst)

				if dstGVK.Kind == "Job" {
					fmt.Println(`Replacing Job "` + dstMetadata.GetName() + `" with CronJob "` + srcMetadata.GetName() + `" in ` + dstMetadata.GetNamespace() + ` namespace`)

					if !execute {
						break
					}

					err := clientset.BatchV1().Jobs(dstMetadata.GetNamespace()).Delete(dstMetadata.GetName(), &metav1.DeleteOptions{PropagationPolicy: &propagationPolicy})

					if err != nil {
						panic(err)
					}

					waitForObjectDeletion(dst, clientset)

					_, err = clientset.BatchV1beta1().CronJobs(srcMetadata.GetNamespace()).Create(src.(*batchv1beta1.CronJob))

					if err != nil {
						panic(err)
					}
				} else if dstGVK.Kind == "CronJob" {
					fmt.Println(`Replacing CronJob "` + dstMetadata.GetName() + `" with CronJob "` + srcMetadata.GetName() + `" in ` + dstMetadata.GetNamespace() + ` namespace`)

					if !execute {
						break
					}
					_, err := clientset.BatchV1beta1().CronJobs(srcMetadata.GetNamespace()).Update(src.(*batchv1beta1.CronJob))

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
