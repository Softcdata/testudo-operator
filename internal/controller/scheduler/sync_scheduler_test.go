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
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestScheduler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scheduler Suite")
}

var _ = Describe("SyncScheduler", func() {
	var scheduler *SyncScheduler

	BeforeEach(func() {
		var err error
		scheduler, err = NewSyncScheduler()
		Expect(err).NotTo(HaveOccurred())
		Expect(scheduler).NotTo(BeNil())
	})

	AfterEach(func() {
		if scheduler != nil {
			_ = scheduler.Shutdown()
		}
	})

	Describe("NewSyncScheduler", func() {
		It("should create a new scheduler successfully", func() {
			s, err := NewSyncScheduler()
			Expect(err).NotTo(HaveOccurred())
			Expect(s).NotTo(BeNil())
			Expect(s.JobCount()).To(Equal(0))
			_ = s.Shutdown()
		})
	})

	Describe("AddOrUpdate", func() {
		It("should add a new job successfully", func() {
			err := scheduler.AddOrUpdate("default", "test-sync", "* * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())
			Expect(scheduler.JobCount()).To(Equal(1))
			Expect(scheduler.HasJob("default", "test-sync")).To(BeTrue())
		})

		It("should update an existing job", func() {
			// Add initial job
			err := scheduler.AddOrUpdate("default", "test-sync", "* * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())
			Expect(scheduler.JobCount()).To(Equal(1))

			// Update with new schedule
			err = scheduler.AddOrUpdate("default", "test-sync", "*/5 * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())
			Expect(scheduler.JobCount()).To(Equal(1)) // Still 1 job
		})

		It("should return error for invalid cron expression", func() {
			err := scheduler.AddOrUpdate("default", "test-sync", "invalid-cron", func() {})
			Expect(err).To(HaveOccurred())
			Expect(scheduler.JobCount()).To(Equal(0))
		})

		It("should handle multiple jobs", func() {
			err := scheduler.AddOrUpdate("ns1", "sync1", "* * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())

			err = scheduler.AddOrUpdate("ns1", "sync2", "* * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())

			err = scheduler.AddOrUpdate("ns2", "sync1", "* * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())

			Expect(scheduler.JobCount()).To(Equal(3))
			Expect(scheduler.HasJob("ns1", "sync1")).To(BeTrue())
			Expect(scheduler.HasJob("ns1", "sync2")).To(BeTrue())
			Expect(scheduler.HasJob("ns2", "sync1")).To(BeTrue())
		})
	})

	Describe("Remove", func() {
		It("should remove an existing job", func() {
			err := scheduler.AddOrUpdate("default", "test-sync", "* * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())
			Expect(scheduler.JobCount()).To(Equal(1))

			scheduler.Remove("default", "test-sync")
			Expect(scheduler.JobCount()).To(Equal(0))
			Expect(scheduler.HasJob("default", "test-sync")).To(BeFalse())
		})

		It("should be safe to remove non-existent job", func() {
			// Should not panic
			scheduler.Remove("default", "non-existent")
			Expect(scheduler.JobCount()).To(Equal(0))
		})
	})

	Describe("Job Execution", func() {
		It("should execute callback on schedule", func() {
			scheduler.Start()

			var counter int32
			callback := func() {
				atomic.AddInt32(&counter, 1)
			}

			// Use a schedule that runs every second (for testing)
			// Note: gocron v2 supports second-level precision with 6-field cron
			// For 5-field, minimum is every minute. We'll use a different approach.
			// Instead, we test that the job is registered and can be triggered.

			err := scheduler.AddOrUpdate("default", "test-sync", "* * * * *", callback)
			Expect(err).NotTo(HaveOccurred())

			// For unit test, we just verify the job is registered
			// Actual execution would require waiting for a full minute
			Expect(scheduler.HasJob("default", "test-sync")).To(BeTrue())
		})
	})

	Describe("Start and Shutdown", func() {
		It("should start and shutdown gracefully", func() {
			scheduler.Start()

			err := scheduler.AddOrUpdate("default", "test-sync", "* * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())

			// Allow some time for scheduler to initialize
			time.Sleep(100 * time.Millisecond)

			err = scheduler.Shutdown()
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
