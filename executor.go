package chanque

import(
  "time"
  "context"
  "sync"
  "sync/atomic"
)

type Job func()

type ExecutorOptionFunc func(*ExecutorOption)

type ExecutorOption struct {
  ctx             context.Context
  panicHandler    PanicHandler
  reducerInterval time.Duration
  maxCapacity     int
}

func ExecutorPanicHandler(handler PanicHandler) ExecutorOptionFunc {
  return func(opt *ExecutorOption) {
    opt.panicHandler = handler
  }
}
func ExecutorReducderInterval(interval time.Duration) ExecutorOptionFunc {
  return func(opt *ExecutorOption) {
    opt.reducerInterval = interval
  }
}
func ExecutorMaxCapacity(capacity int) ExecutorOptionFunc {
  return func(opt *ExecutorOption) {
    opt.maxCapacity = capacity
  }
}
func ExecutorContext(ctx context.Context) ExecutorOptionFunc {
  return func(opt *ExecutorOption) {
    opt.ctx = ctx
  }
}

var(
  defaultReducerInterval = 10 * time.Second
)

type Executor struct {
  mutex           *sync.Mutex
  wg              *sync.WaitGroup
  jobs            *Queue
  ctx             context.Context
  jobCancel       []context.CancelFunc
  healthCancel    context.CancelFunc
  minWorker       int
  maxWorker       int
  panicHandler    PanicHandler
  reducerInterval time.Duration
  runningNum      int32
  workerNum       int32
}

func NewExecutor(minWorker, maxWorker int, funcs ...ExecutorOptionFunc) *Executor {
  opt := new(ExecutorOption)
  for _, fn := range funcs {
    fn(opt)
  }

  if minWorker < 1 {
    minWorker = 0
  }
  if maxWorker < 1 {
    maxWorker = 1
  }
  if maxWorker < minWorker {
    maxWorker = minWorker
  }

  if opt.maxCapacity < 1 {
    opt.maxCapacity = 0
  }
  if opt.panicHandler == nil {
    opt.panicHandler = defaultPanicHandler
  }
  if opt.reducerInterval < 1 {
    opt.reducerInterval = defaultReducerInterval
  }
  if opt.ctx == nil {
    opt.ctx = context.Background()
  }

  e                := new(Executor)
  e.mutex           = new(sync.Mutex)
  e.wg              = new(sync.WaitGroup)
  e.jobs            = NewQueue(opt.maxCapacity, QueuePanicHandler(opt.panicHandler))
  e.ctx             = opt.ctx
  e.jobCancel       = make([]context.CancelFunc, 0)
  e.healthCancel    = nil
  e.minWorker       = minWorker
  e.maxWorker       = maxWorker
  e.panicHandler    = opt.panicHandler
  e.reducerInterval = opt.reducerInterval
  e.runningNum      = int32(0)
  e.workerNum       = int32(0)

  e.initWorker()
  return e
}

func (e *Executor) initWorker() {
  e.mutex.Lock()
  defer e.mutex.Unlock()

  for i := 0; i < e.minWorker; i += 1 {
    e.increWorker()
    jctx, jcancel := context.WithCancel(e.ctx)
    e.jobCancel = append(e.jobCancel, jcancel)

    e.wg.Add(1)
    go e.execloop(jctx, e.jobs)
  }

  hctx, hcancel := context.WithCancel(e.ctx)
  e.healthCancel = hcancel

  e.wg.Add(1)
  go e.healthloop(hctx, e.jobs)
}

func (e *Executor) callPanicHandler(pt PanicType, rcv interface{}) {
  e.panicHandler(pt, rcv)
}

func (e *Executor) increRunning() {
  atomic.AddInt32(&e.runningNum, 1)
}

func (e *Executor) decreRunning() {
  atomic.AddInt32(&e.runningNum, -1)
}

// return num of running workers
func (e *Executor) Running() int32 {
  return atomic.LoadInt32(&e.runningNum)
}

func (e *Executor) increWorker() int32 {
  return atomic.AddInt32(&e.workerNum, 1)
}

func (e *Executor) decreWorker() int32 {
  return atomic.AddInt32(&e.workerNum, -1)
}

// return num of goroutines
func (e *Executor) Workers() int32 {
  return atomic.LoadInt32(&e.workerNum)
}

func (e *Executor) startOndemand() {
  running := int(e.Running())
  if running < e.minWorker {
    return
  }

  next := int(e.increWorker())
  if running < next {
    if e.minWorker < next {
      if next <= e.maxWorker {
        e.mutex.Lock()
        defer e.mutex.Unlock()

        e.wg.Add(1)
        jctx, jcancel := context.WithCancel(e.ctx)
        e.jobCancel = append(e.jobCancel, jcancel)

        go e.execloop(jctx, e.jobs)
        return
      }
    }
  }
  e.decreWorker()
}

// enqueue job
func (e *Executor) Submit(fn Job) {
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

func (e *Executor) ForceStop() {
  e.mutex.Lock()
  defer e.mutex.Unlock()

  for _, cancel := range e.jobCancel {
    cancel()
  }
  e.jobCancel = e.jobCancel[len(e.jobCancel):]
}

// release goroutines
func (e *Executor) Release() {
  defer func(){
    if rcv := recover(); rcv != nil {
      e.callPanicHandler(PanicTypeClose, rcv)
    }
  }()

  e.healthCancel()
  e.jobs.Close()
}

func (e *Executor) ReleaseAndWait() {
  e.Release()
  e.wg.Wait()
}

func (e *Executor) releaseJob(reduceSize int) {
  e.mutex.Lock()
  defer e.mutex.Unlock()

  if reduceSize < 1 {
    return
  }

  cancels := make([]context.CancelFunc, reduceSize)
  copy(cancels, e.jobCancel[0 : reduceSize])
  e.jobCancel = e.jobCancel[reduceSize:]

  for _, cancel := range cancels {
    cancel()
  }
}

func (e *Executor) healthloop(ctx context.Context, jobs *Queue) {
  defer e.wg.Done()
  defer func(){
    if rcv := recover(); rcv != nil {
      e.callPanicHandler(PanicTypeEnqueue, rcv)
    }
  }()

  ticker := time.NewTicker(e.reducerInterval)
  defer ticker.Stop()

  for {
    select {
    case <-ctx.Done():
      return

    case <-ticker.C:
      currentWorkerNum := e.Workers()
      runningWorkerNum := e.Running()
      idleWorkers      := int(currentWorkerNum - runningWorkerNum)
      if e.minWorker < idleWorkers {
        e.releaseJob(int(idleWorkers - e.minWorker))
      }
    }
  }
}

func (e *Executor) execloop(ctx context.Context, jobs *Queue) {
  defer e.wg.Done()
  defer func(){
    if rcv := recover(); rcv != nil {
      e.callPanicHandler(PanicTypeDequeue, rcv)
    }
  }()
  defer e.decreWorker()

  for {
    select {
    case <-ctx.Done():
      return

    case job, ok := <-jobs.Chan():
      if ok != true {
        return
      }

      e.increRunning()
      fn := job.(Job)
      fn()
      e.decreRunning()
    }
  }
}
