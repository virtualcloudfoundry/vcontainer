package interop

import (
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"code.cloudfoundry.org/lager"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
)

func (c *containerInterop) DispatchRunTask(cmd RunTask) (string, error) {
	c.logger.Info("container-interop-dispatch-run-command", lager.Data{"handle": c.handle})
	if err := c.scheduleTask(OneOffTask, &cmd, Run); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return "", verrors.New("failed to create task.")
	}

	return cmd.ID, nil
}

func (c *containerInterop) GetProcessIdFromTaskId(taskId string) (int64, error) {
	c.logger.Info("container-interop-get-pid-from-task-id", lager.Data{"task_id": taskId})
	pidFilePath := c.getPidFilePath(taskId)
	if _, err := os.Stat(pidFilePath); os.IsNotExist(err) {
		c.logger.Info("container-interop-task-pid-file-not-exist", lager.Data{"task_id": taskId})
		return -1, verrors.New("no matching pid file found.")
	} else {
		content, err := ioutil.ReadFile(pidFilePath)
		if err != nil {
			c.logger.Error("container-interop-task-pid-read-file-failed", err, lager.Data{
				"pid_file_path": pidFilePath,
				"task_id":       taskId,
			})
			return -1, verrors.New("get pid file content failed.")
		}
		contentStr := string(content)
		c.logger.Info("container-interop-task-pid-file-content", lager.Data{"pid_file_content": contentStr})
		contentStr = strings.Trim(contentStr, "\n ")
		pid, err := strconv.ParseInt(contentStr, 10, 32)
		if err != nil {
			c.logger.Error("container-interop-task-pid-parse-int-failed", err, lager.Data{
				"pid_file_path": pidFilePath,
				"task_id":       taskId,
				"content":       contentStr,
			})
			return -1, verrors.New("parse pid content failed.")
		} else {
			return pid, nil
		}
	}
}

func (c *containerInterop) WaitForTaskExit(taskId string) error {
	c.logger.Info("container-interop-wait-for-task-exit", lager.Data{"task_id": taskId})
	timeSlept := 0
	for {
		waitResponse, err := c.TaskExited(taskId)
		if err != nil {
			return verrors.New("get task exit info failed.")
		}
		if waitResponse.Exited {
			break
		}
		// sleep for 5 seconds.
		time.Sleep(time.Second * 2)
		timeSlept += 2
		if timeSlept > 60 {
			c.logger.Info("container-interop-wait-for-task-exit-expired", lager.Data{"task_id": taskId})
			break
		}
	}
	return nil
}

// judge whether the task exited.
// this would be running in the vcontainer process.
func (c *containerInterop) TaskExited(taskId string) (vcontainermodels.WaitResponse, error) {
	exitFilePath := c.getTaskExitFilePath(taskId)
	c.logger.Info("container-interop-task-exited-check-file", lager.Data{"file": exitFilePath})
	if _, err := os.Stat(exitFilePath); os.IsNotExist(err) {
		c.logger.Info("container-interop-task-exited-file-not-exist", lager.Data{"task_id": taskId})
		return vcontainermodels.WaitResponse{
			Exited:   false,
			ExitCode: -1,
		}, nil
	} else {
		// TODO racing issue, should lock the file.
		content, err := ioutil.ReadFile(exitFilePath)
		if err != nil {
			c.logger.Error("container-interop-task-exited-read-file-failed", err, lager.Data{
				"exit_file_path": exitFilePath,
				"task_id":        taskId,
			})
			return vcontainermodels.WaitResponse{
				Exited:   false,
				ExitCode: -1,
			}, err
		}
		contentStr := string(content)
		c.logger.Info("container-interop-task-exited-file-content", lager.Data{"exit_file_content": contentStr})
		contentStr = strings.Trim(contentStr, "\n ")
		exitCode, err := strconv.ParseInt(contentStr, 10, 32)
		if err != nil {
			c.logger.Error("container-interop-task-exited-parse-int-failed", err, lager.Data{
				"exit_file_path": exitFilePath,
				"task_id":        taskId,
				"content":        contentStr,
			})
			return vcontainermodels.WaitResponse{
				Exited:   false,
				ExitCode: -1,
			}, err
		}
		return vcontainermodels.WaitResponse{
			Exited:   true,
			ExitCode: int32(exitCode),
		}, nil
	}
}
