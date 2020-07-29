package wmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

var (
	ErrTimeoutExit = errors.New("raise timeout exit")

	errRaisePanic = errors.New("raise panic")

	panicDelay = time.Duration(3 * time.Second)

	deferLogger Logger = new(loggerImpl)
)

const (
	StatusPending = iota
	StatusRunning
	StatusStopped
)

type Logger interface {
	Infof(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

type loggerImpl struct{}

func (l *loggerImpl) Infof(format string, args ...interface{}) {
	fmt.Fprintln(os.Stdout, fmt.Sprintf(format, args...))
}

func (l *loggerImpl) Errorf(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, fmt.Sprintf(format, args...))
}

func SetLogger(log Logger) {
	deferLogger = log
}

type Worker interface {
	Start()        // block mode
	Stop()         // only call worker ctx cancel
	Pname() string // worker process name
	// Status() int   // worker status
}

type WorkerOption struct {
	Recovery bool // avoid app crash exit
	Retries  int  // restart app's times
	Executor Worker
	status   int

	restarted int // already restart count
}

type WorkerManager struct {
	wg sync.WaitGroup

	WorkerList []*WorkerOption
}

func NewWorkerManager() *WorkerManager {
	workerManager := WorkerManager{}
	workerManager.WorkerList = make([]*WorkerOption, 0, 10)
	return &workerManager
}

func (wm *WorkerManager) AddWorker(w Worker) {
	option := &WorkerOption{
		Recovery: true,
		Retries:  0,
		Executor: w,
		status:   StatusPending,
	}
	wm.WorkerList = append(wm.WorkerList, option)
}

func (wm *WorkerManager) AddWorkerWithOption(option *WorkerOption) {
	wm.WorkerList = append(wm.WorkerList, option)
}

func (wm *WorkerManager) Wait() {
	wm.wg.Wait()
}

func (wm *WorkerManager) WaitTimeout(t time.Duration) error {
	var (
		exch = make(chan struct{})
		incr = 0
	)

	inspectFunc := func() []Worker {
		runningWorkers := []Worker{}
		for _, wo := range wm.WorkerList {
			if wo.status == StatusStopped {
				continue
			}

			runningWorkers = append(runningWorkers, wo.Executor)
		}

		return runningWorkers
	}

	go func() {
		backoffer := newBackOff(1 * time.Second)

		for {
			rwlist := inspectFunc()
			if len(rwlist) == 0 { // all workers exit
				close(exch)
				return
			}

			incr++

			if incr < 15 { // fast inspect in 3s
				time.Sleep(200 * time.Millisecond)
				continue
			}

			// counter
			unique := map[string]int{}
			for _, wo := range rwlist {
				pname := wo.Pname()
				cnt, ok := unique[pname]
				if ok {
					unique[pname] = cnt + 1
					continue
				}
				unique[pname] = 1
			}

			for pname, _ := range unique {
				deferLogger.Infof("try to inspect worker %s is stopped in worker manager", pname)
			}

			backoffer.delay()
		}
	}()

	select {
	case <-exch:
		return nil

	case <-time.After(t):
		return ErrTimeoutExit
	}
}

func (wm *WorkerManager) Start() {
	wm.wg.Add(len(wm.WorkerList))
	for _, worker := range wm.WorkerList {
		worker.status = StatusRunning

		go func(wo *WorkerOption) {
			defer func() {
				wo.status = StatusStopped
				wm.wg.Done()
			}()

			for {
				err := wm.tryCatch(wo)
				if err == nil { // safe exit
					return
				}

				if wo.Retries > 0 && wo.restarted >= wo.Retries {
					return
				}

				wo.restarted++

				time.Sleep(panicDelay)
			}

		}(worker)
	}
}

func (wm *WorkerManager) Stop() {
	wm.stop(false)
}

func (wm *WorkerManager) StopAsync() {
	wm.stop(true)
}

func (wm *WorkerManager) stop(async bool) {
	for _, wo := range wm.WorkerList {
		var handle = func(w Worker) {
			defer func() {
				r := recover()
				if r != nil {
					deferLogger.Errorf("worker %s in worker manager error, recover:%v, stack:%v",
						w.Pname(), r, string(Stack()),
					)
				}
			}()

			w.Stop()
		}

		if async {
			go handle(wo.Executor)
		} else {
			handle(wo.Executor)
		}
	}
}

func (wm *WorkerManager) tryCatch(wo *WorkerOption) (err error) {
	// default set err
	err = errRaisePanic

	if wo.Recovery {
		defer func() {
			if r := recover(); r != nil {
				deferLogger.Errorf("worker %s raise panic in worker manager, recover:%v, stack:%v",
					wo.Executor.Pname(), r, string(Stack()),
				)
			} else {
				deferLogger.Infof("worker %s exit safely in worker manager", wo.Executor.Pname())
			}
		}()
	}

	if wo.restarted > 0 {
		deferLogger.Infof("worker %s start %d times in worker manager !!!", wo.Executor.Pname(), wo.restarted)
	} else {
		deferLogger.Infof("worker %s start in worker manager", wo.Executor.Pname())
	}

	wo.Executor.Start()
	return nil
}

func Stack() []byte {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return buf[:n]
}

type backoff struct {
	cur      int
	maxCount int
	period   time.Duration
}

func newBackOff(period time.Duration) *backoff {
	return &backoff{
		cur:      1,
		period:   period,
		maxCount: 10, // default
	}
}

func (b *backoff) delay() {
	if b.cur >= b.maxCount {
		b.cur = b.maxCount
	}

	time.Sleep(time.Duration(b.cur) * b.period)
	b.cur++
}

func IsClosed(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func SleepContext(ctx context.Context, t time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(t):
		return nil
	}
}
