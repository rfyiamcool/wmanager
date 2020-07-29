# wmanager

manage worker in golang, provide start, stop, pname method for worker.

wmanager add trycatch wrapper for each worker.

## Usage

### 1. Your method implements this interface

```go
type Worker interface {
    Start()        // block mode
    Stop()         // only call worker ctx cancel
    Pname() string // worker process name
    Status() int   // worker status
}
```

### 2. new woker manager, add worker

```go
wm = wmanager.NewWorkerManager()

// server
wm.AddWorker(master.NewAPIServer(ctx))
wm.AddWorker(master.NewServer(ctx))

// worker
wm.AddWorker(master.NewHrkeScheduler(ctx))
wm.AddWorker(master.NewCollector(ctx))
```

### 3. start workers

```go
wm.Start()
```

### 4. stop workers

```golang
wm.Stop()
```

### 5. stop workers

```golang
wm.Stop()
```

### 6. wait for all worker exit

```golang
wm.Wait()

or

wm.WaitTimeout(N)
```
