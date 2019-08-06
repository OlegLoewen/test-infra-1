package setup

import (
	"fmt"
	"github.com/gardener/test-infra/integration-tests/e2e/config"
	"github.com/gardener/test-infra/integration-tests/e2e/kubetest"
	"github.com/gardener/test-infra/integration-tests/e2e/util"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"os"
	"path"
	"path/filepath"
)

func Setup() error {
	cleanUpPreviousRuns()
	if areTestUtilitiesReady() {
		log.Info("all test utilities were already ready")
		log.Info("setup finished successfuly. Testutilities ready. Kubetest is ready for usage.")
		return nil
	}

	log.Info("test utilities are not ready. Install...")
	if err := getKubetestAndUtilities(); err != nil {
		return err
	}

	if areTestUtilitiesReady() {
		log.Info("setup finished successfuly. Testutilities ready. Kubetest is ready for usage.")
		return nil
	}
	log.Fatal("Couldn't prepare kubetest utilities")
	return nil
}

func getKubetestAndUtilities() error {
	goModuleOriginValue := os.Getenv("GO111MODULE")
	_ = os.Setenv("GO111MODULE", "off")
	if _, err := util.RunCmd("go get k8s.io/test-infra/kubetest", ""); err != nil {
		return err
	}
	_ = os.Setenv("GO111MODULE", goModuleOriginValue)
	if _, err := util.RunCmd(fmt.Sprintf("kubetest --provider=skeleton --extract=v%s", config.K8sRelease), config.K8sRoot); err != nil {
		return err
	}
	return nil
}

func cleanUpPreviousRuns() {
	if err := os.RemoveAll(config.LogDir); err != nil {
		log.Error(err)
	}
	testResultFiles := util.GetFilesByPattern(config.ExportPath, `test.*\.json$`)
	for _, file := range testResultFiles {
		if err := os.Remove(file); err != nil {
			log.Error(err)
		}
	}
	if err := os.Remove(kubetest.GeneratedRunDescPath); err != nil {
		log.Error(err)
	}
	_ = os.Remove(filepath.Join(config.ExportPath, "started.json"))
	_ = os.Remove(filepath.Join(config.ExportPath, "finished.json"))
	_ = os.Remove(filepath.Join(config.ExportPath, "e2e.log"))
	_ = os.Remove(filepath.Join(config.ExportPath, "junit_01.xml"))
}

func PostRunCleanFiles() error {
	// remove log dir
	if err := os.RemoveAll(config.LogDir); err != nil {
		return err
	}
	// remove kubernetes folder
	if err := os.RemoveAll(os.Getenv("GOPATH")); err != nil {
		return err
	}
	//remove downloads dir
	if err := os.RemoveAll(config.DownloadsDir); err != nil {
		return err
	}
	return nil
}

func areTestUtilitiesReady() bool {
	log.Info("checking whether any test utility is not ready")

	testUtilitiesReady := true
	if !util.CommandExists("kubetest") {
		log.Warn("kubetest not installed")
		testUtilitiesReady = false
	}
	log.Info("kubetest binary available")

	// check if required directories exist
	requiredPaths := [...]string{
		path.Join(config.K8sRoot, "kubernetes/hack"),
		path.Join(config.K8sRoot, "kubernetes/cluster"),
		path.Join(config.K8sRoot, "kubernetes/test"),
		path.Join(config.K8sRoot, "kubernetes/client"),
		path.Join(config.K8sRoot, "kubernetes/server")}
	for _, requiredPath := range requiredPaths {
		if _, err := os.Stat(requiredPath); err != nil {
			log.Warn(errors.Wrapf(err, "dir %s does not exist: ", requiredPath))
			testUtilitiesReady = false
		} else {
			log.Info(fmt.Sprintf("%s dir exists", requiredPath))
		}
	}
	return testUtilitiesReady
}