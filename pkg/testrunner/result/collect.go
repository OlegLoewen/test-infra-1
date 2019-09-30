// Copyright 2019 Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package result

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	tmv1beta1 "github.com/gardener/test-infra/pkg/apis/testmachinery/v1beta1"
	"github.com/gardener/test-infra/pkg/testrunner"
	"github.com/gardener/test-infra/pkg/testrunner/componentdescriptor"
	trerrors "github.com/gardener/test-infra/pkg/testrunner/error"
	"github.com/gardener/test-infra/pkg/util"
	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
)

// Collect collects results of all testruns and writes them to a file.
// It returns whether there are failed testruns or not.
func (c *Collector) Collect(log logr.Logger, tmClient kubernetes.Interface, namespace string, runs testrunner.RunList) (bool, error) {
	var (
		testrunsFailed = false
		result         *multierror.Error
	)
	for _, run := range runs {
		runLogger := log.WithValues("testrun", run.Testrun.Name, "namespace", run.Testrun.Namespace)
		// Do only try to collect testruns results of testruns that ran into a timeout.
		// Any other error can not be retrieved.
		if run.Error != nil && !trerrors.IsTimeout(run.Error) {
			continue
		}

		cfg := c.config
		cfg.OutputDir = filepath.Join(cfg.OutputDir, util.RandomString(3))
		err := Output(runLogger, &cfg, tmClient, namespace, run.Testrun, run.Metadata)
		if err != nil {
			result = multierror.Append(result, err)
			continue
		}

		// ingest into eleasticsearch
		if cfg.OutputDir != "" && cfg.ESConfigName != "" {
			err = IngestDir(runLogger, cfg.OutputDir, cfg.ESConfigName)
			if err != nil {
				runLogger.Error(err, "cannot persist file", "file", cfg.OutputDir)
			} else {
				err := MarkTestrunsAsIngested(runLogger, tmClient, run.Testrun)
				if err != nil {
					runLogger.Error(err, "unable to ingest testrun")
				}
			}
		}

		// upload testrun status to github component
		if cfg.GithubComponentForStatus != "" {
			if cfg.GithubPassword == "" || cfg.GithubUser == "" || cfg.ComponentDescriptorPath == "" {
				runLogger.Error(err, "missing github password / github user / component descriptor path argument")
			}
			components, err := componentdescriptor.GetComponentsFromFile(cfg.ComponentDescriptorPath)

			if component := components.Get(cfg.GithubComponentForStatus); component == nil {
				runLogger.Error(err, "can't find component", "component", cfg.GithubComponentForStatus)
			} else {
				if err := UploadStatusToGithub(run, component, cfg.GithubUser, cfg.GithubPassword); err != nil {
					runLogger.Error(err, "unable to attach testrun status to github component")
				} else {
					err := MarkTestrunsAsUploadedToGithub(runLogger, tmClient, run.Testrun)
					if err != nil {
						runLogger.Error(err, "unable to mark testrun status as uploaded to github")
					}
				}
			}
		}

		if run.Testrun.Status.Phase == tmv1beta1.PhaseStatusSuccess {
			runLogger.Info("Testrun finished successfully")
		} else {
			testrunsFailed = true
			runLogger.Error(fmt.Errorf("Testrun failed with phase %s", run.Testrun.Status.Phase), "")
		}
		fmt.Print(util.PrettyPrintStruct(run.Testrun.Status))
		util.RenderStatusTable(os.Stdout, run.Testrun.Status.Steps)
	}

	c.fetchTelemetryResults()

	return testrunsFailed, util.ReturnMultiError(result)
}

func (c *Collector) fetchTelemetryResults() {
	if c.telemetry != nil {
		c.log.Info("fetch telemetry controller summaryPath")
		_, err := c.telemetry.StopAndAnalyze("", "text")
		if err != nil {
			c.log.Error(err, "unable to fetch telemetry measurements")
			return
		}
	}
}
