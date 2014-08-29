timer
=====

[![Build Status](https://travis-ci.org/pivotal-golang/timer.svg?branch=master)](https://travis-ci.org/pivotal-golang/timer)

Timer is a wrapper interface around go's `time` package. It allows time to be manipulated in tests.

### Usage

Instantiate a `Timer`, and pass it into the function you'll be testing:

```go
import "github.com/pivotal-golang/timer"

// ...

MyFunction(timer.New())
```

You can use the `Timer` to sleep, and to create one-time or recurring time channels:

```go
func MyFunction(timer timer.Timer) {
  timer.Sleep(3 * time.Second)
  println("Done Sleeping")

  oneTime := timer.After(time.Minute)
  recurring := timer.Every(time.Second)

  for {
    select {
    case <-recurring:
      println("Tick")
    case <-oneTime:
      println("Tock")
      return
    }
  }
}
```

### Writing tests

Now that your function is using a `Timer`, you can test it using a `FakeTimer`:

```go
import "github.com/pivotal-golang/timer/fake_timer"

// ...

fakeTimer := fake_timer.New(time.Now())
go MyFunction(fakeTimer)
```

You can now control the passage of time:

```go
fakeTimer.Elapse(3 * time.Second)

// output:
// Done Sleeping

fakeTimer.Elapse(time.Second)

// output:
// Tick

fakeTimer.Elapse(time.Minute)

// output:
// Tick
// Tock
```



### License

Timer is [Apache 2.0](https://github.com/pivotal-golang/lager/blob/master/LICENSE) licensed.
