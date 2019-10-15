package kubetest

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gardener/test-infra/integration-tests/e2e/config"
	"github.com/gardener/test-infra/integration-tests/e2e/util"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	E2eLogFileNamePattern   = "e2e.log$"
	JunitXmlFileNamePattern = `junit_\d+.xml$`
	TestSummaryFileName     = "test_summary.json"
	MergedJunitXmlFile      = "junit_01.xml"
	MergedE2eLogFile        = "build-log.txt"
)

var mergedJunitXmlFilePath = filepath.Join(config.ExportPath, MergedJunitXmlFile)
var MergedE2eLogFilePath = filepath.Join(config.ExportPath, MergedE2eLogFile)

// Analyze analyzes junit.xml files and e2e.log files, which are dumped by kubetest and provides a resulting test suite summary and results for each testcase individually. These results are then written to the export dir as files.
func Analyze(kubetestResultsPath string) Summary {
	log.Infof("Analyze e2e.log and junit.xml files in %s", kubetestResultsPath)

	e2eLogFilePaths := util.GetFilesByPattern(kubetestResultsPath, E2eLogFileNamePattern)
	summary, err := analyzeE2eLogs(e2eLogFilePaths)
	if err != nil {
		log.Fatal(errors.Wrapf(err, "results analysis failed at e2e.log analysis"))
	}

	junitXMLFilePaths := util.GetFilesByPattern(kubetestResultsPath, JunitXmlFileNamePattern)
	if err := analyzeJunitXMLsEnrichSummary(junitXMLFilePaths, &summary); err != nil {
		log.Fatal(errors.Wrapf(err, "results analysis failed at junit.xml analysis"))
	}

	log.Infof("test suite summary: %+v\n", summary)
	writeSummaryToFile(summary)
	log.Infof("Check out result files in %s", config.ExportPath)

	return summary
}

func writeSummaryToFile(summary Summary) {
	file, err := json.Marshal(summary)
	file = append([]byte("{\"index\": {\"_index\": \"e2e_testsuite\", \"_type\": \"_doc\"}}\n"), file...)
	if err != nil {
		log.Fatal(errors.Wrapf(err, "couldn't marshal testsuite summary %s", summary))
	}
	summaryFilePath := path.Join(config.ExportPath, TestSummaryFileName)
	if err := ioutil.WriteFile(summaryFilePath, file, 0644); err != nil {
		log.Fatal(errors.Wrapf(err, "Couldn't write %s to file", summaryFilePath))
	}
}

func analyzeJunitXMLsEnrichSummary(junitXMLFilePaths []string, summary *Summary) error {
	var mergedJunitXmlResult = &JunitXMLResult{FailedTests: 0, ExecutedTests: 0, DurationFloat: 0, SuccessfulTests: 0, DurationInt: summary.TestsuiteDuration}
	testcaseNameToTestcase := make(map[string]TestcaseResult)
	failureOccurrences := make(map[string]int)
	for _, junitXMLPath := range junitXMLFilePaths {
		file, err := os.Open(junitXMLPath)
		if err != nil {
			return err
		}
		defer file.Close()
		junitXml, err := UnmarshalJunitXMLResult(file.Name())
		if err != nil {
			return errors.Wrapf(err, "Couldn't unmarshal %s", file.Name())
		}
		mergedJunitXmlResult.FailedTests += junitXml.FailedTests
		mergedJunitXmlResult.ExecutedTests += junitXml.ExecutedTests
		mergedJunitXmlResult.SuccessfulTests += junitXml.SuccessfulTests
		for _, testcase := range junitXml.Testcases {
			if testcase.Skipped {
				if _, ok := testcaseNameToTestcase[testcase.Name]; !ok {
					testcaseNameToTestcase[testcase.Name] = testcase
				}
				continue
			}
			if !testcase.Successful && !testcase.Skipped {
				if _, ok := failureOccurrences[testcase.Name]; ok {
					failureOccurrences[testcase.Name]++
				} else {
					failureOccurrences[testcase.Name] = 1
				}
			}
			testcaseNameToTestcase[testcase.Name] = testcase
			testcaseJSON, err := json.Marshal(testcase)
			if err != nil {
				return errors.Wrapf(err, "Couldn't marshal testsuite summary %s", testcaseJSON)
			}
			testcaseJSON = append([]byte("{\"index\": {\"_index\": \"e2e_testcase\", \"_type\": \"_doc\"}}\n"), testcaseJSON...)

			jsonFileName := fmt.Sprintf("test-%s.json", strconv.FormatInt(time.Now().UnixNano(), 10))
			testcaseJsonFilePath := path.Join(config.ExportPath, jsonFileName)
			if err := ioutil.WriteFile(testcaseJsonFilePath, testcaseJSON, 0644); err != nil {
				return errors.Wrapf(err, "Couldn't write %s to file", testcaseJsonFilePath)
			}
		}
	}
	for _, testcase := range testcaseNameToTestcase {
		mergedJunitXmlResult.Testcases = append(mergedJunitXmlResult.Testcases, testcase)
	}
	addFlakynessInfoToSummary(summary, &failureOccurrences)
	if err := saveJunitXmlToFile(mergedJunitXmlResult); err != nil {
		return err
	}
	return nil
}

func addFlakynessInfoToSummary(summary *Summary, failureOccurrences *map[string]int) {
	for testcaseName, failureOccurrence := range *failureOccurrences {
		if failureOccurrence < config.FlakeAttempts {
			// testcase had failures and successes
			summary.FlakedTestcases++
		} else {
			// testcase has failed in all attempts
			summary.FailedTestcaseNames = append(summary.FailedTestcaseNames, testcaseName)
		}
	}
	summary.FailedTestcases = len(summary.FailedTestcaseNames)
	summary.TestsuiteSuccessful = summary.FailedTestcases == 0
	if summary.FlakedTestcases != 0 {
		summary.Flaked = true
	}
}

func saveJunitXmlToFile(mergedJunitXmlResult *JunitXMLResult) error {
	output, err := xml.MarshalIndent(mergedJunitXmlResult, "  ", "    ")
	if err != nil {
		return err
	}
	output = append([]byte(xml.Header), output...)

	file, _ := os.Create(mergedJunitXmlFilePath)
	defer file.Close()
	if _, err = file.Write(output); err != nil {
		return err
	}
	return nil
}

func analyzeE2eLogs(e2eLogFilePaths []string) (Summary, error) {
	mergeE2eLogFiles(MergedE2eLogFilePath, e2eLogFilePaths)

	emptySummary := Summary{DescriptionFile: config.DescriptionFile}
	summary := emptySummary
	regexpRanSpecs := regexp.MustCompile(`Ran (?P<TestcasesRan>\d+).*Specs.in`)
	regexpPassedFailed := regexp.MustCompile(`(?P<Passed>\d+) Passed.*?(?P<Failed>\d+) Failed.*Pending`)
	regexpGinkgoRanIn := regexp.MustCompile(`Ginkgo ran \d+ suite in (?P<TestSuiteDuration>.+)`)

	for _, e2eLogPath := range e2eLogFilePaths {
		file, err := os.Open(e2eLogPath)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		for lineByte := range util.ReadLinesFromFile(file) {
			lineString := string(lineByte)
			if regexpRanSpecs.MatchString(lineString) {
				groupToValue := util.GetGroupMapOfRegexMatches(regexpRanSpecs, lineString)
				groupToValueInt, err := convertValuesToInt(groupToValue)
				if err != nil {
					return summary, errors.Wrapf(err, "Empty or non integer values in map, for regexp '%s'", regexpRanSpecs.String())
				}
				summary.ExecutedTestcases += groupToValueInt["TestcasesRan"]
			}
			if regexpPassedFailed.MatchString(lineString) {
				groupToValue := util.GetGroupMapOfRegexMatches(regexpPassedFailed, lineString)
				groupToValueInt, err := convertValuesToInt(groupToValue)
				if err != nil {
					return summary, errors.Wrapf(err, "Empty or non integer values in map, for regexp '%s'", regexpPassedFailed.String())
				}
				summary.SuccessfulTestcases += groupToValueInt["Passed"]
			}
			if regexpGinkgoRanIn.MatchString(lineString) {
				log.Info(lineString)
				groupToValue := util.GetGroupMapOfRegexMatches(regexpGinkgoRanIn, lineString)
				duration, err := time.ParseDuration(groupToValue["TestSuiteDuration"])
				if err != nil {
					return summary, err
				}
				summary.TestsuiteDuration += int(duration.Seconds())
			}
		}
		if summary.isEmpty() {
			contentBytes, err := ioutil.ReadFile(e2eLogPath)
			if err != nil {
				log.Fatal(err)
			}
			log.Fatalf("Wasn't able to interpret e2e.log. Got only zero values. Check e2e.log output:\n%s", string(contentBytes))
		}
	}
	summary.ExecutionGroup = strings.Join(config.TestcaseGroup, ",")
	summary.FinishedTime = time.Now()
	summary.StartTime = summary.FinishedTime.Add(time.Second * time.Duration(-summary.TestsuiteDuration))

	return summary, nil
}

func convertValuesToInt(m map[string]string) (map[string]int, error) {
	convertedMap := make(map[string]int, len(m))
	for key, value := range m {
		if key == "" {
			continue // ignore fields without a key
		}
		convertedValue, err := strconv.Atoi(value)
		if err != nil {
			return nil, err
		}
		convertedMap[key] = convertedValue
	}
	return convertedMap, nil
}

func mergeE2eLogFiles(dst string, e2eLogFilePaths []string) {
	resultFile, _ := os.Create(dst)

	for _, e2eLogFile := range e2eLogFilePaths {
		fileToAppend, err := os.Open(e2eLogFile)
		if err != nil {
			log.Fatalln("failed to open file %s for reading:", e2eLogFile, err)
		}
		defer fileToAppend.Close()

		if _, err := io.Copy(resultFile, fileToAppend); err != nil {
			log.Fatalln("failed to append file %s to file %s:", fileToAppend, resultFile, err)
		}
	}
	if len(e2eLogFilePaths) == 1 {
		log.Infof("copied %s file to %s/%s", e2eLogFilePaths[0], dst, MergedE2eLogFile)
	} else {
		log.Infof("merged %o e2e log files to %s%s", len(e2eLogFilePaths), dst, MergedE2eLogFile)
	}
}

type Summary struct {
	ExecutedTestcases   int       `json:"executed_testcases"`
	SuccessfulTestcases int       `json:"successful_testcases"`
	FailedTestcases     int       `json:"failed_testcases"`
	FlakedTestcases     int       `json:"flaked_testcases"`
	Flaked              bool      `json:"individual_testcases_flaked"`
	TestsuiteDuration   int       `json:"duration"`
	TestsuiteSuccessful bool      `json:"successful"`
	DescriptionFile     string    `json:"test_desc_file"`
	StartTime           time.Time `json:"-"`
	FinishedTime        time.Time `json:"-"`
	ExecutionGroup      string    `json:"execution_group"`
	FailedTestcaseNames []string  `json:"failed_testcase_names"`
}

func (summary Summary) isEmpty() bool {
	return summary.ExecutedTestcases == 0 &&
		summary.SuccessfulTestcases == 0 &&
		summary.FailedTestcases == 0 &&
		summary.FlakedTestcases == 0 &&
		summary.Flaked == false &&
		summary.TestsuiteSuccessful == false &&
		len(summary.FailedTestcaseNames) == 0
}
