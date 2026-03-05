/*
Copyright 2024 NVIDIA

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

package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Runner", func() {
	var (
		runner       Runner
		service1     *FakeRunnable
		service2     *FakeRunnable
		ctx          context.Context
		service1Name = "service1"
		service2Name = "service2"
	)
	BeforeEach(func() {
		runner = New()
		service1 = NewFakeRunnable()
		service2 = NewFakeRunnable()
		ctx = context.Background()
	})
	AfterEach(func() {
		Expect(service1.Started()).To(BeTrue())
		Expect(service2.Started()).To(BeTrue())
		Expect(service1.Stopped()).To(BeTrue())
		Expect(service2.Stopped()).To(BeTrue())
	})
	Context("Basic", func() {
		BeforeEach(func() {
			runner.AddService(service1Name, service1)
			runner.AddService(service2Name, service2)
		})
		It("Service start error", func() {
			done := make(chan interface{})
			go func() {
				defer GinkgoRecover()
				service1.SetFunction(func(ctx context.Context, _ func()) error {
					time.Sleep(time.Millisecond * 100)
					return fmt.Errorf("test error")
				})
				go func() {
					defer GinkgoRecover()
					Expect(runner.Run(ctx)).To(HaveOccurred())
				}()
				Expect(errors.Is(runner.Wait(ctx), context.Canceled)).To(BeTrue())
				close(done)
			}()
			Eventually(done, 30).Should(BeClosed())
		})
		It("Service complete without error", func() {
			done := make(chan interface{})
			go func() {
				defer GinkgoRecover()
				service1.SetFunction(func(ctx context.Context, _ func()) error {
					time.Sleep(time.Millisecond * 100)
					return nil
				})
				Expect(runner.Run(ctx)).NotTo(HaveOccurred())
				close(done)
			}()
			Eventually(done, 5).Should(BeClosed())
		})
		It("Run/Stop", func() {
			done := make(chan interface{})
			go func() {
				defer GinkgoRecover()
				ctx, cFunc := context.WithCancel(ctx)
				go func() {
					time.Sleep(time.Millisecond * 100)
					cFunc()
				}()
				Expect(runner.Run(ctx)).NotTo(HaveOccurred())
				close(done)
			}()
			Eventually(done, 5).Should(BeClosed())
		})
		It("Wait for readiness - only one service ready", func() {
			done := make(chan interface{})
			go func() {
				defer GinkgoRecover()
				service1.SetFunction(func(ctx context.Context, readyFunc func()) error {
					time.Sleep(time.Millisecond * 100)
					readyFunc()
					<-ctx.Done()
					return nil
				})
				service2.SetFunction(func(ctx context.Context, readyFunc func()) error {
					<-ctx.Done()
					return nil
				})
				rCtx, rCFunc := context.WithCancel(ctx)
				wg := sync.WaitGroup{}
				wg.Add(1)

				go func() {
					defer GinkgoRecover()
					Expect(runner.Run(rCtx)).NotTo(HaveOccurred())
					wg.Done()
				}()
				timeoutCtx, cFunc := context.WithTimeout(ctx, time.Second)
				defer cFunc()
				// wait should return error
				Expect(runner.Wait(timeoutCtx)).To(HaveOccurred())
				Expect(service1.Ready()).To(BeTrue())
				Expect(service2.Ready()).To(BeFalse())
				rCFunc()
				wg.Wait()
				close(done)
			}()
			Eventually(done, 5).Should(BeClosed())
		})
		It("Wait for readiness - both services ready", func() {
			done := make(chan interface{})
			go func() {
				defer GinkgoRecover()
				getWaitFunc := func() func(ctx context.Context, readyFunc func()) error {
					return func(ctx context.Context, readyFunc func()) error {
						time.Sleep(time.Millisecond * 100)
						readyFunc()
						<-ctx.Done()
						return nil
					}
				}
				service1.SetFunction(getWaitFunc())
				service2.SetFunction(getWaitFunc())

				rCtx, rCFunc := context.WithCancel(ctx)
				wg := sync.WaitGroup{}
				wg.Add(1)
				go func() {
					defer GinkgoRecover()
					Expect(runner.Run(rCtx)).NotTo(HaveOccurred())
					wg.Done()
				}()
				Expect(runner.Wait(ctx)).NotTo(HaveOccurred())
				Expect(service1.Ready()).To(BeTrue())
				Expect(service2.Ready()).To(BeTrue())
				rCFunc()
				wg.Wait()
				close(done)
			}()
			Eventually(done, 5).Should(BeClosed())
		})
	})
})
