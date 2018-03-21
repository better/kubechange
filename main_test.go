package main

import (
	"testing"
	batchv2alpha1 "k8s.io/client-go/pkg/apis/batch/v2alpha1"
	batchv1 "k8s.io/client-go/pkg/apis/batch/v1"
	v1 "k8s.io/client-go/pkg/api/v1"
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

	var fooSuccessfulJobsHistoryLimit int32 = 1
	var fooSuspend bool = true
	var fooActiveDeadlineSeconds int64 = 90
	var barSuccessfulJobsHistoryLimit int32 = 2
	var barFailedJobsHistoryLimit int32 = 3
	cronJobFoo := batchv2alpha1.CronJob{
		Spec: batchv2alpha1.CronJobSpec{
			Schedule: "* * * * *",
			Suspend: &fooSuspend,
			SuccessfulJobsHistoryLimit: &fooSuccessfulJobsHistoryLimit,
			JobTemplate: batchv2alpha1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					ActiveDeadlineSeconds: &fooActiveDeadlineSeconds,
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							RestartPolicy: "Always",
							Containers: []v1.Container{
								{
									Name: "example",
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

	cronJobBar := batchv2alpha1.CronJob{
		Spec: batchv2alpha1.CronJobSpec{
			Schedule: "1 * * * *",
			SuccessfulJobsHistoryLimit: &barSuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit: &barFailedJobsHistoryLimit,
			JobTemplate: batchv2alpha1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "example",
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

	{
		fields := deepCompareCronJobSpec(cronJobFoo.Spec, cronJobBar.Spec)

		if len(fields) == 0 {
			t.Errorf("Failed to correctly compare fields between CronJobs")
		} else {
			expectedFields := map[string]bool {
				"activeDeadlineSeconds": false,
				"schedule": false,
				"successfulJobsHistoryLimit": false,
				"failedJobsHistoryLimit": false,
				"suspend": false,
				"nodeSelector": false,
				"restartPolicy": false,
				"containers": false,
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

func TestPlan(t *testing.T) {
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
