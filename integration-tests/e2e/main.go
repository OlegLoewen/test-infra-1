package main

import (
	"bufio"
	"github.com/gardener/test-infra/integration-tests/e2e/config"
	"github.com/gardener/test-infra/integration-tests/e2e/kubetest"
	"github.com/gardener/test-infra/integration-tests/e2e/kubetest/setup"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
)

func main() {
	if err := setup.Setup(); err != nil {
		log.Fatal(errors.Wrapf(err, "Initial setup invocation failed"))
	}
	desc := kubetest.Generate()
	kubetestResultsPath := kubetest.Run(desc)
	resultSummary := kubetest.Analyze(kubetestResultsPath)

	if config.RetryFailedTestcases {
		if err := os.RemoveAll(config.ExportPath); err != nil {
			log.Fatalf("failed to clean dir '%s': %s", config.ExportPath, err)
		}
		if err := os.MkdirAll(config.ExportPath, os.FileMode(0777)); err != nil {
			log.Fatalf("failed to create dir '%s': %s", config.ExportPath, err)
		}
		desc = createDescFileOfFailedTestcases(resultSummary.FailedTestcaseNames)
		kubetestResultsPath = kubetest.Run(desc)
		resultSummary = kubetest.Analyze(kubetestResultsPath)
	}
	if config.PublishResultsToTestgrid == true && resultSummary.TestsuiteSuccessful == true {
		kubetest.Publish(config.ExportPath, resultSummary)
	}
	if config.RunCleanUpAfterTest {
		if err := setup.PostRunCleanFiles(); err != nil {
			log.Fatal(errors.Wrapf(err, "cleaning up downloaded artifacts and kubernetes folder failed"))
		}
	}
	if !resultSummary.TestsuiteSuccessful {
		log.Fatalf("e2e testsuite failed for %d testcases: %v", resultSummary.FailedTestcases, resultSummary.FailedTestcaseNames)
	}
}

func createDescFileOfFailedTestcases(failedTestcases []string) string {
	generatedRunDescPath := filepath.Join(config.TmpDir, "failedTestcasesDescription.txt")
	file, err := os.OpenFile(generatedRunDescPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		log.Fatalf("failed creating file: %s", err)
	}
	datawriter := bufio.NewWriter(file)
	for _, testcase := range failedTestcases {
		_, _ = datawriter.WriteString(testcase + "\n")
	}
	if err := datawriter.Flush(); err != nil {
		log.Fatalf("failed to flush data writer %s", err)
	}
	if err := file.Close(); err != nil {
		log.Fatalf("failed to close file %s", err)
	}

	return file.Name()
}
