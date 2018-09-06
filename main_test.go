package main

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	fakeclientset "k8s.io/client-go/kubernetes/fake"
)

func getExampleCronJobs() (batchv1beta1.CronJob, batchv1beta1.CronJob) {
	var fooSuccessfulJobsHistoryLimit int32 = 1
	var fooSuspend bool = true
	var fooActiveDeadlineSeconds int64 = 90
	var barSuccessfulJobsHistoryLimit int32 = 2
	var barFailedJobsHistoryLimit int32 = 3

	cronJobFoo := batchv1beta1.CronJob{
		Spec: batchv1beta1.CronJobSpec{
			Schedule:                   "* * * * *",
			Suspend:                    &fooSuspend,
			SuccessfulJobsHistoryLimit: &fooSuccessfulJobsHistoryLimit,
			JobTemplate: batchv1beta1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					ActiveDeadlineSeconds: &fooActiveDeadlineSeconds,
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							RestartPolicy: "Always",
							Containers: []v1.Container{
								{
									Name:  "example",
									Image: "scratch",
								},
							},
							NodeSelector: map[string]string{
								"group": "prod",
							},
						},
					},
				},
			},
		},
	}

	cronJobBar := batchv1beta1.CronJob{
		Spec: batchv1beta1.CronJobSpec{
			Schedule:                   "1 * * * *",
			SuccessfulJobsHistoryLimit: &barSuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     &barFailedJobsHistoryLimit,
			JobTemplate: batchv1beta1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "example",
									Image: "scratch2",
								},
							},
							NodeSelector: map[string]string{
								"group": "staging",
							},
						},
					},
				},
			},
		},
	}

	return cronJobFoo, cronJobBar
}

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

func TestCompare(t *testing.T) {
	fields := compareNodeSelector(map[string]string{
		"group": "prod",
	}, map[string]string{
		"group": "staging",
	})

	if len(fields) != 1 {
		t.Errorf("Failed comparing node selectors")
	} else if fields[0] != "group" {
		t.Errorf("Incorrect node selector comparison result")
	}

	cronJobFoo, cronJobBar := getExampleCronJobs()

	{
		fields := deepCompareObject(runtime.Object(&cronJobFoo), runtime.Object(&cronJobBar))

		if len(fields) == 0 {
			t.Errorf("Failed to correctly compare fields between CronJobs")
		} else {
			expectedFields := map[string]bool{
				"activeDeadlineSeconds":      false,
				"schedule":                   false,
				"successfulJobsHistoryLimit": false,
				"failedJobsHistoryLimit":     false,
				"suspend":                    false,
				"nodeSelector":               false,
				"restartPolicy":              false,
				"containers":                 false,
			}

			for _, field := range fields {
				if _, expected := expectedFields[field]; expected {
					expectedFields[field] = true
				} else {
					t.Error("Unexpected field " + field)
				}
			}

			someFieldsNotFound := false

			for field, found := range expectedFields {
				if found == false {
					someFieldsNotFound = true
					t.Log("Expected field " + field + " was not found in spec comparison")
				}
			}

			if someFieldsNotFound {
				t.Fail()
			}
		}
	}
}

func TestPrePlan(t *testing.T) {
	files := readFiles([]string{"example-test-job.yml"})
	for _, file := range files {
		objects, _ := parseManifests(file)
		filteredObjects := filterObjectsByLabel(objects, "kronjob/job")

		if len(filteredObjects) != 1 {
			t.Errorf("Failed to filter objects by existing label")
		}

		namespaces := getObjectNamespaces(objects)

		if len(namespaces) != 1 {
			t.Errorf("Failed to get namespaces from manifests")
		} else if namespaces[0] != "default" {
			t.Errorf("Incorrect namespace extracted from manifests")
		}

		pairs := pairObjectsByCriteria(objects, objects, PairCriteria{label: "kronjob/job"})

		if len(pairs) != 1 {
			t.Errorf("Failed to pair objects")
		} else {
			pair := pairs[0]

			if pair.src == nil || pair.dst == nil {
				t.Errorf("Invalid pair")
			}
		}
	}
}

func TestPlan(t *testing.T) {
	clientset := fakeclientset.NewSimpleClientset()

	cronJobFoo, cronJobBar := getExampleCronJobs()
	foo := runtime.Object(&cronJobFoo)
	bar := runtime.Object(&cronJobBar)

	{
		pair := ObjectPair{&foo, &bar}
		plan := generatePlan([]ObjectPair{pair})

		if len(plan) != 1 {
			t.Errorf("Invalid plan generated")
		}

		if plan[0].action != "update" {
			t.Errorf("Incorrect plan action, expected update")
		}

		executePlan(plan, PlanConfig{clientset, false})
	}

	{
		pair := ObjectPair{&foo, nil}
		plan := generatePlan([]ObjectPair{pair})

		if len(plan) != 1 {
			t.Errorf("Invalid plan generated")
		}

		if plan[0].action != "create" {
			t.Errorf("Incorrect plan action, expected create")
		}

		executePlan(plan, PlanConfig{clientset, false})
	}

}
