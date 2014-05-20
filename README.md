[![Build Status](https://travis-ci.org/cloudfoundry-incubator/executor.svg?branch=master)](https://travis-ci.org/cloudfoundry-incubator/executor)
[![Coverage Status](https://coveralls.io/repos/cloudfoundry-incubator/executor/badge.png?branch=HEAD)](https://coveralls.io/r/cloudfoundry-incubator/executor?branch=HEAD)

![Executor Dreadnaught Class](http://img1.wikia.nocookie.net/__cb20130614094003/factpile/images/d/d9/Executor.jpg)

#Executor
it's time to play the game


## Usage
1) Start Warden-Linux locally with vagrant and virtualbox

```bash
# download
go get -u -v github.com/cloudfoundry-incubator/warden-linux
cd ~/go/src/github.com/cloudfoundry-incubator/warden-linux

# start warden
vagrant up
scripts/run-warden-remote-linux
# password: vagrant
```

2) Start Loggregator

```bash
# download
go get -v github.com/cloudfoundry/loggregator
cd ~/go/src/github.com/cloudfoundry/loggregator
git submodule update --init --recursive

# compile for mac os x
GOPATH=~/go/src/github.com/cloudfoundry/loggregator && PLATFORMS="darwin/amd64" bin/build-platforms

# start loggregator
release/loggregator-darwin-amd64 --config ~/go/src/github.com/cloudfoundry-incubator/executor/loggregator-config.json
```

3) Run Executor locally

```bash
# download
go get -v github.com/cloudfoundry-incubator/executor
cd executor

# start executor
scripts/run-local
```
