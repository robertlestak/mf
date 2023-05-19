package mf

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	ps "github.com/mitchellh/go-ps"
	log "github.com/sirupsen/logrus"
)

type State int

const (
	Running State = iota
	Stopped
)

type Process struct {
	Command       string
	Pid           int
	Children      []int
	CheckCommand  string
	CheckDelay    time.Duration
	CheckInterval time.Duration
	CheckTimeout  time.Duration

	State State
	cmd   *exec.Cmd
}

func (p *Process) Check() error {
	l := log.WithFields(log.Fields{
		"fn":  "mf.Process.Check",
		"pid": p.Pid,
	})
	l.Debug("checking process")
	if p.Pid == 0 {
		l.Debug("process already stopped")
		return nil
	}
	if p.CheckCommand == "" {
		l.Debug("no check command provided")
		return nil
	}
	// split check command into command and args
	cs := strings.Split(p.CheckCommand, " ")
	cmd := exec.Command(cs[0], cs[1:]...)
	cmd.Env = os.Environ()
	// capture output and stderr to variables
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		outStr := out.String()
		errMsg := errOut.String()
		// join output and stderr
		if outStr != "" {
			errMsg = strings.Join([]string{errMsg, outStr}, "\n")
		}
		l.WithError(err).Debugf("process check failed: %s", errMsg)
		return err
	}
	l.Debug("process checked")
	return nil
}

func pidExists(pid int) bool {
	l := log.WithFields(log.Fields{
		"fn":  "mf.pidExists",
		"pid": pid,
	})
	l.Debug("checking if pid exists")
	if pid == 0 {
		l.Debug("process already stopped")
		return false
	}
	killErr := syscall.Kill(pid, syscall.Signal(0))
	return killErr == nil
}

func (p *Process) PidExists() bool {
	l := log.WithFields(log.Fields{
		"fn":  "mf.Process.PidExists",
		"pid": p.Pid,
	})
	l.Debug("checking if pid exists")
	if p.Pid == 0 {
		l.Debug("process already stopped")
		return false
	}
	return pidExists(p.Pid)
}

func (p *Process) Checker() {
	l := log.WithFields(log.Fields{
		"fn":  "mf.Process.Checker",
		"pid": p.Pid,
	})
	l.Debug("starting checker")
	if p.CheckCommand == "" {
		l.Debug("no check command provided")
		return
	}
	var failureTime time.Time
	for {
		time.Sleep(p.CheckDelay)
		l.Debug("checking process")
		// check if pid is active
		if !p.PidExists() {
			l.Debug("process not found")
			return
		}
		if err := p.Check(); err != nil {
			if p.State == Running {
				l.WithField("err", err).Info("process check failed, stopping process")
				if err := p.Stop(); err != nil {
					l.WithError(err).Error("failed to stop process")
				}
			} else {
				l.WithField("err", err).Info("process check failed")
			}
			if failureTime.IsZero() {
				failureTime = time.Now()
			}
			if p.CheckTimeout > 0 && time.Since(failureTime) > p.CheckTimeout {
				l.WithField("err", err).Info("process check failed, check timeout reached")
				if err := p.Exit(); err != nil {
					l.WithError(err).Error("failed to stop process")
				}
			}
		} else if p.State == Stopped {
			l.Info("process check passed, resuming process")
			if err := p.Resume(); err != nil {
				l.WithError(err).Error("failed to resume process")
			}
			failureTime = time.Time{}
		}
		time.Sleep(p.CheckInterval)
	}
}

func getChildren(list []ps.Process, pid int) ([]int, error) {
	l := log.WithFields(log.Fields{
		"fn":  "mf.getChildren",
		"pid": pid,
	})
	l.Debug("getting children")
	if pid == 0 {
		l.Debug("process already stopped")
		return nil, nil
	}
	var children []int
	for _, proc := range list {
		if proc.PPid() == pid {
			children = append(children, proc.Pid())
		}
	}
	l.WithField("children", children).Debug("got children")
	return children, nil
}

func containsInt(s []int, e int) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func getChildrenWorker(list []ps.Process, pids chan int, children chan []int) {
	l := log.WithFields(log.Fields{
		"fn": "mf.getChildrenWorker",
	})
	for pid := range pids {
		l.WithField("pid", pid).Debug("getting children")
		c, err := getChildren(list, pid)
		if err != nil {
			l.WithError(err).Error("failed to get children")
			continue
		}
		children <- c
	}
}

func (p *Process) GetChildrenRecursive() ([]int, error) {
	l := log.WithFields(log.Fields{
		"fn":  "mf.Process.GetChildrenRecursive",
		"pid": p.Pid,
	})
	l.Debug("getting children recursively")
	if p.Pid == 0 {
		l.Debug("process already stopped")
		return nil, nil
	}
	// get all children of this process, and their children, etc.
	var children []int
	var checked []int
	var err error
	list, err := ps.Processes()
	if err != nil {
		l.WithError(err).Error("failed to get processes")
		return nil, err
	}
	children, err = getChildren(list, p.Pid)
	if err != nil {
		l.WithError(err).Error("failed to get children")
		return nil, err
	}
	allchildren := children
	checked = append(checked, p.Pid)
	var newchildren []int
	newchildren = children
	for len(newchildren) > 0 {
		var newnewchildren []int
		pids := make(chan int)
		children := make(chan []int)
		for i := 0; i < 10; i++ {
			go getChildrenWorker(list, pids, children)
		}
		for _, child := range newchildren {
			if containsInt(checked, child) {
				continue
			}
			checked = append(checked, child)
			pids <- child
		}
		close(pids)
		for i := 0; i < len(newchildren); i++ {
			c := <-children
			newnewchildren = append(newnewchildren, c...)
		}
		newchildren = newnewchildren
		allchildren = append(allchildren, newnewchildren...)
	}
	l.WithField("children", allchildren).Debug("got children recursively")
	p.Children = allchildren
	return allchildren, nil
}

func (p *Process) Start() error {
	cs := strings.Split(p.Command, " ")
	var command string
	var args []string
	if len(cs) > 0 {
		command = cs[0]
	}
	if len(cs) > 1 {
		args = cs[1:]
	}
	l := log.WithFields(log.Fields{
		"fn":   "mf.Process.Start",
		"cmd":  command,
		"args": args,
	})
	l.Debug("starting process")
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		l.WithError(err).Error("failed to start process")
		return err
	}
	p.Pid = cmd.Process.Pid
	p.cmd = cmd
	p.State = Running
	l.Debug("process started")
	return nil
}

func (p *Process) Wait() error {
	l := log.WithFields(log.Fields{
		"fn":  "mf.Process.Wait",
		"pid": p.Pid,
	})
	l.Debug("waiting for process")
	if p.Pid == 0 {
		l.Debug("process already stopped")
		return nil
	}
	if err := p.cmd.Wait(); err != nil {
		l.WithError(err).Error("failed to wait for process")
		return err
	}
	l.Debug("process finished")
	return nil
}

func stopProcess(pid int) error {
	l := log.WithFields(log.Fields{
		"fn":  "mf.stopProcess",
		"pid": pid,
	})
	l.Debug("stopping process")
	if pid == 0 {
		l.Debug("process already stopped")
		return nil
	}
	// send kill -TSTP to pid
	if err := syscall.Kill(pid, syscall.SIGTSTP); err != nil {
		l.WithError(err).Error("failed to stop process")
		return err
	}
	l.Debug("process stopped")
	return nil
}

func (p *Process) Stop() error {
	l := log.WithFields(log.Fields{
		"fn":  "mf.Process.Stop",
		"pid": p.Pid,
	})
	l.Debug("stopping process")
	if p.Pid == 0 {
		l.Debug("process already stopped")
		return nil
	}
	if p.State == Stopped {
		l.Debug("process already stopped")
		return nil
	}
	childrencomplete := make(chan struct{})
	go func() {
		children, err := p.GetChildrenRecursive()
		if err != nil {
			l.WithError(err).Error("failed to get children pids")
			return
		}
		childcomplete := make(chan struct{}, len(children))
		for _, child := range children {
			l.WithField("child", child).Debug("stopping child process")
			go func(child int) {
				if err := stopProcess(child); err != nil {
					l.WithError(err).Error("failed to stop child process")
				}
				childcomplete <- struct{}{}
			}(child)
		}
		for i := 0; i < len(children); i++ {
			<-childcomplete
		}
		childrencomplete <- struct{}{}
	}()
	// stop process
	if err := stopProcess(p.Pid); err != nil {
		l.WithError(err).Error("failed to stop process")
		return err
	}
	p.State = Stopped
	l.Debug("waiting for children to stop")
	<-childrencomplete
	l.Debug("process stopped")
	return nil
}

func resumeProcess(pid int) error {
	l := log.WithFields(log.Fields{
		"fn":  "mf.resumeProcess",
		"pid": pid,
	})
	l.Debug("resuming process")
	if pid == 0 {
		l.Debug("process already stopped")
		return nil
	}
	// send kill -CONT to pid
	if err := syscall.Kill(pid, syscall.SIGCONT); err != nil {
		l.WithError(err).Error("failed to resume process")
		return err
	}
	l.Debug("process resumed")
	return nil
}

func (p *Process) Resume() error {
	l := log.WithFields(log.Fields{
		"fn":  "mf.Process.Resume",
		"pid": p.Pid,
	})
	l.Debug("resuming process")
	if p.Pid == 0 {
		l.Debug("process already stopped")
		return nil
	}
	if p.State == Running {
		l.Debug("process already running")
		return nil
	}
	// resume process
	if err := resumeProcess(p.Pid); err != nil {
		l.WithError(err).Error("failed to resume process")
		return err
	}
	// resume children
	resumed := make(chan struct{}, len(p.Children))
	for _, child := range p.Children {
		l.WithField("child", child).Debug("resuming child process")
		go func(child int) {
			if err := resumeProcess(child); err != nil {
				l.WithError(err).Error("failed to resume child process")
			}
			resumed <- struct{}{}
		}(child)
	}
	for i := 0; i < len(p.Children); i++ {
		<-resumed
	}
	p.State = Running
	l.Debug("process resumed")
	return nil
}

func exitProcess(pid int) error {
	l := log.WithFields(log.Fields{
		"fn":  "mf.exitProcess",
		"pid": pid,
	})
	l.Debug("exiting process")
	if pid == 0 {
		l.Debug("process already stopped")
		return nil
	}
	// send kill -TERM to pid
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		l.WithError(err).Error("failed to exit process")
		return err
	}
	l.Debug("process exited")
	return nil
}

func (p *Process) Exit() error {
	l := log.WithFields(log.Fields{
		"fn":  "mf.Process.Exit",
		"pid": p.Pid,
	})
	l.Debug("exiting process")
	if p.Pid == 0 {
		l.Debug("process already stopped")
		return nil
	}
	// exit process
	if err := exitProcess(p.Pid); err != nil {
		l.WithError(err).Error("failed to exit process")
		return err
	}
	l.Debug("process exited")
	return nil
}
