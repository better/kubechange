package main

import (
	"encoding/json"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

//todo: get field names from json tags
//todo: consider comparing json (or deserialized json) instead of direct fields
//todo: consider implementing visitor pattern similar to kubectl for comparisons

func deepCompareObject(src runtime.Object, dst runtime.Object) []string {
	var fields []string
	srcGVK := getObjectGroupVersionKind(src)
	dstGVK := getObjectGroupVersionKind(dst)

	if src == nil || dst == nil || srcGVK.String() != dstGVK.String() {
		return []string{"kind"}
	}

	switch srcType := src.(type) {
	case *batchv1.Job:
		dstJob := dst.(*batchv1.Job)
		return deepCompareJobSpec(srcType.Spec, dstJob.Spec)
	case *batchv1beta1.CronJob:
		dstCronJob := dst.(*batchv1beta1.CronJob)
		return deepCompareCronJobSpec(srcType.Spec, dstCronJob.Spec)
	}

	return fields
}

func deepCompareCronJobSpec(src batchv1beta1.CronJobSpec, dst batchv1beta1.CronJobSpec) []string {
	var fields []string

	if src.Schedule != dst.Schedule {
		fields = append(fields, "schedule")
	}

	if src.ConcurrencyPolicy != dst.ConcurrencyPolicy {
		fields = append(fields, "concurrencyPolicy")
	}

	if src.Suspend != nil {
		if dst.Suspend == nil || *src.Suspend != *dst.Suspend {
			fields = append(fields, "suspend")
		}
	} else if dst.Suspend != nil {
		fields = append(fields, "suspend")
	}

	if src.SuccessfulJobsHistoryLimit != nil {
		if dst.SuccessfulJobsHistoryLimit == nil || *src.SuccessfulJobsHistoryLimit != *dst.SuccessfulJobsHistoryLimit {
			fields = append(fields, "successfulJobsHistoryLimit")
		}
	} else if dst.SuccessfulJobsHistoryLimit != nil {
		fields = append(fields, "successfulJobsHistoryLimit")
	}

	if src.FailedJobsHistoryLimit != nil {
		if dst.FailedJobsHistoryLimit == nil || *src.FailedJobsHistoryLimit != *dst.FailedJobsHistoryLimit {
			fields = append(fields, "failedJobsHistoryLimit")
		}
	} else if dst.FailedJobsHistoryLimit != nil {
		fields = append(fields, "failedJobsHistoryLimit")
	}

	fields = append(fields, deepCompareJobTemplateSpec(src.JobTemplate, dst.JobTemplate)...)

	return fields
}

func deepCompareJobTemplateSpec(src batchv1beta1.JobTemplateSpec, dst batchv1beta1.JobTemplateSpec) []string {
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
