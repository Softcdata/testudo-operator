/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scheduler

import (
	"fmt"
	"sync"

	"github.com/go-co-op/gocron/v2"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SyncScheduler 管理 DataSync 和 ResourceSync 资源的 cron 定时调度。
// 使用 gocron 库处理 cron 表达式和任务管理。
// 当 operator pod 重启时，调度会通过 Controller 的 Reconcile 循环自动恢复
// （Informer 同步所有 CR，触发重新注册）。
type SyncScheduler struct {
	scheduler gocron.Scheduler
	jobs      map[string]gocron.Job // key: "{namespace}/{name}", value: job
	schedules map[string]string     // key: "{namespace}/{name}", value: schedule expression
	mu        sync.RWMutex
	log       logr.Logger
}

// NewSyncScheduler 创建一个新的 SyncScheduler 实例。
func NewSyncScheduler() (*SyncScheduler, error) {
	s, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("创建 gocron 调度器失败: %w", err)
	}

	return &SyncScheduler{
		scheduler: s,
		jobs:      make(map[string]gocron.Job),
		schedules: make(map[string]string),
		log:       ctrl.Log.WithName("sync-scheduler"),
	}, nil
}

// jobKey 生成用于标识调度任务的唯一键。
func jobKey(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// AddOrUpdate 添加新的 cron 任务或更新现有任务。
// 此方法是幂等的 - 使用相同的 key 多次调用只会更新调度和回调。
//
// 参数:
//   - namespace: CR 的命名空间（DataSync 或 ResourceSync）
//   - name: CR 的名称
//   - schedule: cron 表达式（5字段标准格式，例如 "*/15 * * * *"）
//   - callback: 调度触发时执行的函数
func (s *SyncScheduler) AddOrUpdate(namespace, name, schedule string, callback func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := jobKey(namespace, name)

	// 检查是否已存在相同的调度
	if currentSchedule, exists := s.schedules[key]; exists {
		if currentSchedule == schedule {
			// 调度未变更，无需操作
			return nil
		}
		// 调度变更，需要移除旧任务
		if existingJob, jobExists := s.jobs[key]; jobExists {
			if err := s.scheduler.RemoveJob(existingJob.ID()); err != nil {
				s.log.Error(err, "移除现有任务失败", "key", key)
			} else {
				s.log.V(1).Info("为更新移除了现有任务", "key", key)
			}
			delete(s.jobs, key)
			delete(s.schedules, key)
		}
	}

	// 添加新的 cron 调度任务
	job, err := s.scheduler.NewJob(
		gocron.CronJob(schedule, false), // false = 5字段标准格式
		gocron.NewTask(callback),
		gocron.WithName(key),
	)
	if err != nil {
		return fmt.Errorf("为 %s 添加 cron 任务失败，调度表达式 %q: %w", key, schedule, err)
	}

	s.jobs[key] = job
	s.schedules[key] = schedule
	s.log.Info("已注册 cron 任务", "key", key, "schedule", schedule, "jobID", job.ID())

	return nil
}

// Remove 根据命名空间和名称移除调度任务。
func (s *SyncScheduler) Remove(namespace, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := jobKey(namespace, name)

	if job, exists := s.jobs[key]; exists {
		if err := s.scheduler.RemoveJob(job.ID()); err != nil {
			s.log.Error(err, "移除任务失败", "key", key)
		} else {
			s.log.Info("已移除 cron 任务", "key", key)
		}
		delete(s.jobs, key)
		delete(s.schedules, key)
	}
}

// HasJob 检查给定命名空间和名称是否存在任务。
func (s *SyncScheduler) HasJob(namespace, name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := jobKey(namespace, name)
	_, exists := s.jobs[key]
	return exists
}

// JobCount 返回当前调度任务的数量。
func (s *SyncScheduler) JobCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs)
}

// Start 启动调度器。任务将按其调度开始执行。
func (s *SyncScheduler) Start() {
	s.scheduler.Start()
	s.log.Info("SyncScheduler 已启动")
}

// Shutdown 优雅地关闭调度器并等待正在运行的任务完成。
func (s *SyncScheduler) Shutdown() error {
	s.log.Info("正在关闭 SyncScheduler...")
	if err := s.scheduler.Shutdown(); err != nil {
		return fmt.Errorf("关闭调度器失败: %w", err)
	}
	s.log.Info("SyncScheduler 关闭完成")
	return nil
}
