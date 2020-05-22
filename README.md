# `chanque`

[![MIT License](https://img.shields.io/github/license/mashape/apistatus.svg?maxAge=2592000)](https://github.com/octu0/chanque/blob/master/LICENSE)
[![GoDoc](https://godoc.org/github.com/octu0/chanque?status.svg)](https://godoc.org/github.com/octu0/chanque)
[![Go Report Card](https://goreportcard.com/badge/github.com/octu0/chanque)](https://goreportcard.com/report/github.com/octu0/chanque)
[![Releases](https://img.shields.io/github/v/release/octu0/chanque)](https://github.com/octu0/chanque/releases)

`chanque` provides a simple workload for channel and goroutine.

## Documentation

https://godoc.org/github.com/octu0/chanque

## Installation

```
$ go get github.com/octu0/chanque
```

## Usage

### Queue

Queue implementation.  
It provides blocking and non-blocking methods, as well as panic handling of channels.

```go
import(
  "fmt"
  "github.com/octu0/chanque"
)

func main(){
  que1 := chanque.NewQueue(10)
  defer que1.Close()

  go func(){
    for {
      val := que1.Dequeue()
      fmt.Println(val.(string))
    }
  }()
  if ok := que1.Enqueue("hello"); ok {
    fmt.Println("enqueue-ed")
  }

  que2 := chanque.NewQueue(10)
  que2.PanicHandler(func(rcv interface{}, ret *bool) {
    switch err.(type) {
    case error:
      fmt.Println("panic occurred")
    }
  })
  if ok := que2.EnqueueNB("world w/ non-blocking enqueue"); ok {
    fmt.Println("enqueue-ed")
  }
}
```

### Executor

WorkerPool implementation,  
which limits the number of concurrent executions of goroutines and creates goroutines as needed,  
and can also be used as goroutine resource management.

```go
import(
  "fmt"
  "time"
  "github.com/octu0/chanque"
)

func main(){
  // minWorker 1 maxWorker 2 
  exec := chanque.CreateExecutor(1, 2) 
  defer exec.Release()

  exec.Submit(func(){
    fmt.Println("job1")
    time.Sleep(1 * time.Second)
  })
  exec.Submit(func(){
    fmt.Println("job2")
    time.Sleep(1 * time.Second)
  })

  // Blocking because it became the maximum number of workers, 
  // executing when there are no more workers running
  exec.Submit(func(){
    fmt.Println("job3")
  })

  // Generate goroutines on demand up to the maximum number of workers.
  // Submit does not block up to the size of MaxCapacity
  // Workers that are not running are recycled to minWorker at the time of ReduceInterval.
  exec2 := chanque.CreateExecutor(10, 50,
    chanque.ExecutorMaxCapaticy(1000),
    chanque.ExecutorReducderInterval(60 * time.Second),
  )
  defer exec2.Release()

  for i := 0; i < 100; i += 1 {
    exec2.Submit(func(i id) func() {
      return func() {
        fmt.Println("heavy process", id)
        time.Sleep(100 * time.Millisecond)
        fmt.Println("done process", id)
      }
    }(i))
  }
}
```

### Worker

Worker implementation for asynchronous execution, register WorkerHandler and execute it with Enqueue parameter.  
Enqueue of parameter is blocked while WorkerHandler is running.  
There is also a BufferWorker implementation that does not block during asynchronous execution.

```go
import(
  "fmt"
  "time"
  "context"
  "github.com/octu0/chanque"
)

func main(){
  handler := func(param interface{}) {
    if s, ok := param.(string); ok {
      fmt.Println(s)
    }
    time.Sleep(1 * time.Second)
  }
  w1 := chanque.NewDefaultWorker(handler)
  w1.Run(context.TODO())
  defer w1.Shutdown()

  go func(){
    w1.Enqueue("hello")
    w1.Enqueue("world") // blocking for 1 sec
  }()

  w2 := chanque.NewBufferWorker(handler, 10)
  w2.Run(context.TODO())
  defer w2.Shutdown()

  go func(){
    w2.Enqueue("hello")
    w2.Enqueue("world") // non-blocking
  }()
}

```

## Functions

### type `Queue`

```
NewQueue(capacity int) *Queue

func(*Queue) Enqueue(value interface{}) (written bool)
func(*Queue) EnqueueNB(value interface{}) (written bool)
func(*Queue) EnqueueRetry(value interface{}, interval time.Duration, retry int) (written bool)

func(*Queue) Dequeue() (value interface{}, found bool)
func(*Queue) DequeueNB() (value interface{}, found bool)
func(*Queue) DequeueRetry(interval time.Duration, retry int) (value interface{}, found bool)
```

### type `Executor`

```
type Job func()

CreateExecutor(minWorker, maxWorker int) *Executor

func(*Executor) Submit(Job)
func(*Executor) Release()
func(*Executor) ReleaseAndWait()
func(*Executor) Running() int32
func(*Executor) Workers() int32
```

### type `Worker`

```
type WorkerHandler func(parameter interface{})

NewDefaultWorker(handler WorkerHandler)
NewBufferWorker(handler WorkerHandler, minWorker, maxWorker int)

func(Worker) Run(context.Context)
func(Worker) Enqueue(parameter interface{})
func(Worker) Shutdown()
```
