package worker

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewCommandLogger", func() {
	It("should fail if stderr is already set", func() {
		cmd := exec.Command("")
		cmd.Stderr = io.Discard

		_, err := NewCommandLogger(cmd, logr.Discard())
		Expect(err).To(HaveOccurred())
	})

	It("should fail if stdout is already set", func() {
		cmd := exec.Command("")
		cmd.Stdout = io.Discard

		_, err := NewCommandLogger(cmd, logr.Discard())
		Expect(err).To(HaveOccurred())
	})

	It("should return a non-nil objet", func() {
		Expect(
			NewCommandLogger(exec.Command(""), logr.Discard()),
		).NotTo(
			BeNil(),
		)
	})
})

const (
	helperEnv = "_TEST_HELPER_PROCESS"
	stderrMsg = "stderr-msg"
	stdoutMsg = "stdout-msg"
)

type logItem struct {
	Logger string `json:"logger"`
	Msg    string `json:"msg"`
}

var _ = Describe("CommandLogger_Wait", func() {
	It("should log entries correctly", func() {
		cmd := exec.Command(os.Args[0], "-test.run=TestExecutorHelper", ".")
		cmd.Env = []string{helperEnv + "=1"}

		msgs := make(map[string]string)

		mutex := sync.Mutex{}

		logFunc := func(obj string) {
			r := strings.NewReader(obj)
			li := logItem{}

			Expect(
				json.NewDecoder(r).Decode(&li),
			).NotTo(
				HaveOccurred(),
			)

			mutex.Lock()
			msgs[li.Logger] = li.Msg
			mutex.Unlock()
		}

		logger := funcr.NewJSON(logFunc, funcr.Options{})

		cl, err := NewCommandLogger(cmd, logger)
		Expect(err).NotTo(HaveOccurred())

		Expect(cmd.Start()).NotTo(HaveOccurred())

		Expect(cl.Wait()).NotTo(HaveOccurred())
		Expect(cmd.Wait()).NotTo(HaveOccurred())

		expected := map[string]string{"stderr": stderrMsg, "stdout": stdoutMsg}

		Expect(msgs).To(Equal(expected))
	})
})

func TestExecutorHelper(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}

	fmt.Fprint(os.Stdout, stdoutMsg)
	fmt.Fprint(os.Stderr, stderrMsg)

	// Go writes PASS at the end of execution.
	// Redirect stdout to /dev/null to prevent this and match output accurately.
	os.Stdout, _ = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
}
