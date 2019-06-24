package kubetest

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/gardener/test-infra/test/e2etest/config"
	"github.com/gardener/test-infra/test/e2etest/util"
	"github.com/gardener/test-infra/test/e2etest/util/sets"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"path/filepath"
)

const (
	FalsePositivesDescFile = "false_positives.json"
	SkipDescFile           = "skip.json"
	GeneratedRunDescFile   = "generated_tests_to_run.txt"
	AllTestcasesFile       = "all_testcases.txt"
	Success                = "success"
	Failure                = "failure"
	Wildcard               = "*"
)

var falsePositiveDescPath = filepath.Join(config.DescriptionsPath, FalsePositivesDescFile)
var GeneratedRunDescPath = filepath.Join(config.TmpDir, GeneratedRunDescFile)
var AllTestcasesFilePath = filepath.Join(config.TmpDir, AllTestcasesFile)
var skipDescPath = filepath.Join(config.DescriptionsPath, SkipDescFile)

func Generate() (desc string) {
	log.Info("Generate test description file")
	testcasesToRun := sets.NewStringSet()
	allE2eTestcases := getAllE2eTestCases()

	if config.DescriptionFilePath != "" {
		testcasesFromDescFile := UnmarshalDescription(config.DescriptionFilePath)
		for _, testcaseFromDesc := range testcasesFromDescFile {
			matching := allE2eTestcases.GetMatchingForTestcase(testcaseFromDesc.Name, testcaseFromDesc.Skip, testcaseFromDesc.Focus)
			if testcaseFromDesc.validForCurrentContext() {
				if matching.Len() == 0 {
					log.Warnf("Couldn't find testcase: '%s'", testcaseFromDesc.Name)
					continue
				}
				testcasesToRun = testcasesToRun.Union(matching)
			} else {
				// this is necessary since e.g. all conformance testcases are added by a wildcard, but there may still be
				// additionally a conformance test excluded explicitly or assigned to a group
				testcasesToRun = testcasesToRun.Difference(matching)
			}
		}
	}

	if config.RetestFlaggedOnly {
		var testcasesFromDescriptionFile = sets.NewStringSet()
		testcasesFromDescriptionFile.Insert("[Conformance]")
		testcasesToRun = allE2eTestcases.GetMatchingOfSet(testcasesFromDescriptionFile)
	}

	if !config.IgnoreFalsePositiveList {
		falsePositiveTestcases := getTestcaseNamesFromDesc(falsePositiveDescPath)
		for falsePositiveTestcase := range falsePositiveTestcases {
			testcasesToRun.Delete(falsePositiveTestcase)
		}
	}

	if !config.IgnoreSkipList {
		skipTestcases := getTestcaseNamesFromDesc(skipDescPath)
		for skipTestcase := range skipTestcases {
			testcasesToRun.DeleteMatching(skipTestcase)
		}
	}

	if config.IncludeUntrackedTests {
		untrackedTestcases := allE2eTestcases
		trackedTestcases := sets.NewStringSet()
		descFiles := util.GetFilesByPattern(config.DescriptionsPath, `\.json`)
		for _, descFile := range descFiles {
			trackedTestcases.Union(getTestcaseNamesFromDesc(descFile))
		}
		for trackedTestcase := range trackedTestcases {
			untrackedTestcases.DeleteMatching(trackedTestcase)
		}
		testcasesToRun.Union(untrackedTestcases)
	}

	if len(testcasesToRun) == 0 {
		log.Fatal("No testcases found to run.")
	}

	// write testcases to run to file
	if err := writeLinesToFile(testcasesToRun, GeneratedRunDescPath); err != nil {
		log.Fatal(errors.Wrapf(err, "Couldn't save testcasesToRun as file in %s", GeneratedRunDescPath))
	}
	log.Infof("Description file %s generated", GeneratedRunDescPath)
	return GeneratedRunDescPath
}

func getTestcaseNamesFromDesc(descPath string) sets.StringSet {
	matchedTestcases := sets.NewStringSet()
	testcases := UnmarshalDescription(descPath)
	for _, testcase := range testcases {
		if len(testcase.ExcludedProviders) != 0 && len(testcase.OnlyProviders) != 0 {
			log.Warn("fields excluded and only of description file testcase, are not allowed to be defined both at the same time. Skipping testcase: %s", testcase.Name)
			continue
		}
		if testcase.validForCurrentContext() {
			matchedTestcases.Insert(testcase.Name)
		}
	}
	return matchedTestcases
}

func (testcase TestcaseDesc) validForCurrentContext() bool {
	validForCurrentContext := false
	excludedExplicitly := util.Contains(testcase.ExcludedProviders, config.CloudProvider)
	consideredByOnlyField := testcase.OnlyProviders == nil || len(testcase.OnlyProviders) != 0 && util.Contains(testcase.OnlyProviders, config.CloudProvider)
	testcasesGroupMatched := false
	for _, testcaseGroup := range config.TestcaseGroup {
		if testcaseGroup == Wildcard || util.Contains(testcase.TestcaseGroups, testcaseGroup) {
			testcasesGroupMatched = true
			break
		}
	}
	retestActiveForThisProviderAndTest := config.RetestFlaggedOnly && util.Contains(testcase.Retest, config.CloudProvider)
	if !excludedExplicitly && consideredByOnlyField && !config.RetestFlaggedOnly && testcasesGroupMatched || retestActiveForThisProviderAndTest {
		validForCurrentContext = true
	}
	return validForCurrentContext
}

func getAllE2eTestCases() sets.StringSet {
	allTestcases := sets.NewStringSet()
	resultsPath := DryRun()
	defer os.RemoveAll(resultsPath)
	junitPaths := util.GetFilesByPattern(resultsPath, JunitXmlFileNamePattern)
	if len(junitPaths) > 1 {
		log.Fatalf("Found multiple junit.xml files after dry run of kubetest in %s. Expected only one.", resultsPath)
	}
	if len(junitPaths) == 0 {
		log.Fatalf("No junit file has been created during kubetest dry run.", resultsPath)
	}
	junitXml, err := UnmarshalJunitXMLResult(junitPaths[0])
	if err != nil {
		log.Fatal(errors.Wrapf(err, "Couldn't unmarshal junit.xml %s", junitPaths[0]))
	}

	// get testcase names of all not skipped testcases
	for _, testcase := range junitXml.Testcases {
		if !testcase.Skipped {
			allTestcases.Insert(testcase.Name)
		}
	}
	if log.GetLevel() == log.DebugLevel {
		allTestcases.WriteToFile(AllTestcasesFilePath)
	}
	return allTestcases
}

func UnmarshalDescription(descPath string) []TestcaseDesc {
	var testcases []TestcaseDesc
	descFile, err := ioutil.ReadFile(descPath)
	if err != nil {
		log.Fatal(errors.Wrapf(err, "Couldn't read file %s: %s", descPath, descFile))
	}
	if err = json.Unmarshal(descFile, &testcases); err != nil {
		log.Fatal(errors.Wrapf(err, "Couldn't unmarshal %s: %s", descPath, descFile))
	}
	return testcases
}

func UnmarshalJunitXMLResult(junitXmlPath string) (junitXml JunitXMLResult, err error) {
	var xmlResult JunitXMLResult
	junitXML, err := ioutil.ReadFile(junitXmlPath)
	if err != nil {
		return xmlResult, err
	}
	if err = xml.Unmarshal(junitXML, &xmlResult); err != nil {
		return xmlResult, err
	}

	xmlResult.CalculateAdditionalFields()
	return xmlResult, nil
}

// writeLinesToFile writes the lines to the given file.
func writeLinesToFile(lines sets.StringSet, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	for line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			log.Warn(errors.Wrapf(err, "Couldn't write text '%s' to file %s", line, file))
		}
	}
	return w.Flush()
}

type TestcaseDesc struct {
	Name              string   `json:"testcase"`
	ExcludedProviders []string `json:"exclude,omitempty"`
	OnlyProviders     []string `json:"only,omitempty"`
	Retest            []string `json:"retest,omitempty"`
	TestcaseGroups    []string `json:"groups"`
	Skip              string   `json:"skip,omitempty"`
	Focus             string   `json:"focus,omitempty"`
}
