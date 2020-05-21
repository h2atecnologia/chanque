package chanque

import(
  "time"
  "sync"
  "sync/atomic"
)

type Job func()

type Executor struct {
  mutex        *sync.Mutex
  wg           *sync.WaitGroup
  jobs         *Queue
  done         *Queue
  minWorker    int
  maxWorker    int
  panicHandler PanicHandler
  runningNum   int32
  workerNum    int32
}

func CreateExecutor(minWorker, maxWorker int) *Executor {
  if minWorker < 1 {
    minWorker = 0
  }
  if maxWorker < 1 {
    maxWorker = 1
  }
  if maxWorker < minWorker {
    maxWorker = minWorker
  }

  e             := new(Executor)
  e.mutex        = new(sync.Mutex)
  e.wg           = new(sync.WaitGroup)
  e.jobs         = NewQueue(maxWorker)
  e.done         = NewQueue(0)
  e.minWorker    = minWorker
  e.maxWorker    = maxWorker
  e.panicHandler = defaultPanicHandler
  e.runningNum   = int32(0)
  e.workerNum    = int32(0)
  e.initWorker()
  return e
}
func (e *Executor) initWorker() {
  for i := 0; i < e.minWorker; i += 1 {
    e.wg.Add(1)
    go e.execloop(e.jobs)
  }
  e.wg.Add(1)
  go e.healthloop(e.done, e.jobs)
}
func (e *Executor) PanicHandler(handler PanicHandler) {
  e.mutex.Lock()
  defer e.mutex.Unlock()

  e.panicHandler = handler
  e.jobs.PanicHandler(handler)
  e.done.PanicHandler(handler)
}
func (e *Executor) callPanicHandler(pt PanicType, rcv interface{}) {
  e.mutex.Lock()
  defer e.mutex.Unlock()

  e.panicHandler(pt, rcv)
}
func (e *Executor) increRunning() {
  atomic.AddInt32(&e.runningNum, 1)
}
func (e *Executor) decreRunning() {
  atomic.AddInt32(&e.runningNum, -1)
}
func (e *Executor) Running() int32 {
  return atomic.LoadInt32(&e.runningNum)
}
func (e *Executor) increWorker() {
  atomic.AddInt32(&e.workerNum, 1)
}
func (e *Executor) decreWorker() {
  atomic.AddInt32(&e.workerNum, 1)
}
func (e *Executor) Workers() int32 {
  return atomic.LoadInt32(&e.workerNum)
}
func (e *Executor) startOndemand() {
  e.mutex.Lock()
  next := int(e.Running() + 1)
  if e.minWorker < next {
    if next < e.maxWorker {
      e.wg.Add(1)
      go e.execloop(e.jobs)
    }
  }
  e.mutex.Unlock()
}
func (e *Executor) Submit(fn func()) {
  defer func(){
    if rcv := recover(); rcv != nil {
      e.callPanicHandler(PanicTypeEnqueue, rcv)
    }
  }()

  if fn == nil {
    return
  }

  e.startOndemand()
  e.jobs.Enqueue(fn)
}
func (e *Executor) Release() {
  defer func(){
    if rcv := recover(); rcv != nil {
      e.callPanicHandler(PanicTypeClose, rcv)
    }
  }()

  e.done.Close()
  e.jobs.Close()
}
func (e *Executor) ReleaseAndWait() {
  e.Release()
  e.wg.Wait()
}
func (e *Executor) healthloop(done *Queue, jobs *Queue) {
  defer e.wg.Done()
  defer func(){
    if rcv := recover(); rcv != nil {
      e.callPanicHandler(PanicTypeEnqueue, rcv)
    }
  }()

  ticker := time.NewTicker(10 * time.Second)
  defer ticker.Stop()

  for {
    select {
    case <-done.Chan():
      return

    case <-ticker.C:
      currentWorkerNum := e.Workers()
      runningWorkerNum := e.Running()
      idleWorkers      := int(currentWorkerNum - runningWorkerNum)
      if e.minWorker < idleWorkers {
        reduceSize := int(idleWorkers - e.minWorker)
        for i := 0; i < reduceSize; i += 1 {
          jobs.EnqueueNB(nil)
        }
      }
    }
  }
}
func (e *Executor) execloop(jobs *Queue) {
  defer e.wg.Done()
  defer func(){
    if rcv := recover(); rcv != nil {
      e.callPanicHandler(PanicTypeDequeue, rcv)
    }
  }()

  e.increWorker()
  defer e.decreWorker()

  for {
    select {
    case job, ok := <-jobs.Chan():
      if ok != true {
        return
      }
      if job == nil {
        return
      }

      e.increRunning()
      fn := job.(Job)
      fn()
      e.decreRunning()
    }
  }
}
