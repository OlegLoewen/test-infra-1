package kubetest

import (
	"encoding/xml"
	"github.com/gardener/test-infra/integration-tests/e2e/config"
	"regexp"
	"strings"
)

func (result *JunitXMLResult) CalculateAdditionalFields() {
	result.SuccessfulTests = result.ExecutedTests - result.FailedTests
	result.DurationInt = int(result.DurationFloat)
	regexpSigGroup := regexp.MustCompile(`^\[.*?]`)
	for i, _ := range result.Testcases {
		result.Testcases[i].calculateAdditionalFields(regexpSigGroup)
		// TODO: why is here result.Testcases[i].calculateAdditionalFields(regexpSigGroup) not possible, why is assigning to variable is required?
	}
}

func (testcase *TestcaseResult) calculateAdditionalFields(regexpSigGroup *regexp.Regexp) {
	testcase.SigGroup = regexpSigGroup.FindString(testcase.Name)
	if testcase.SkippedRaw != nil {
		testcase.Skipped = true
	}
	if testcase.FailureText == "" {
		testcase.Status = Success
		testcase.Successful = true
	} else {
		testcase.Status = Failure
		testcase.Successful = false
	}
	testcase.DurationInt = int(testcase.DurationFloat)
	testcase.TestDesc = config.DescriptionFile
	testcase.ExecutionGroup = strings.Join(config.TestcaseGroup, ",")
}

type JunitXMLResult struct {
	XMLName         xml.Name         `xml:"testsuite"`
	ExecutedTests   int              `xml:"tests,attr"`
	FailedTests     int              `xml:"failures,attr"`
	DurationFloat   float32          `xml:"time,attr"`
	Testcases       []TestcaseResult `xml:"testcase"`
	DurationInt     int              `xml:"-"` // calculated
	SuccessfulTests int              `xml:"-"` // calculated
}

type TestcaseResult struct {
	XMLName        xml.Name  `xml:"testcase" json:"-"`
	Name           string    `xml:"name,attr" json:"name"`
	Status         string    `xml:"-" json:"status"` // calculated
	SkippedRaw     *struct{} `xml:"skipped" json:"-"`
	Skipped        bool      `xml:"-" json:"-"` // calculated
	FailureText    string    `xml:"failure,omitempty" json:"failure.text,omitempty"`
	SystemOutput   string    `xml:"system-out,omitempty" json:"system-out,omitempty"`
	DurationFloat  float32   `xml:"time,attr" json:"-"`
	DurationInt    int       `xml:"-" json:"duration"`        // calculated
	SigGroup       string    `xml:"-" json:"sig"`             // calculated
	TestDesc       string    `xml:"-" json:"test_desc_file"`  // calculated
	ExecutionGroup string    `xml:"-" json:"execution_group"` // calculated
	Successful     bool      `xml:"-" json:"successful"`      // calculated
}
