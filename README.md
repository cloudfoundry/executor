[![Build Status](https://travis-ci.org/cloudfoundry-incubator/executor.svg?branch=master)](https://travis-ci.org/cloudfoundry-incubator/executor)
[![Coverage Status](https://coveralls.io/repos/cloudfoundry-incubator/executor/badge.png?branch=HEAD)](https://coveralls.io/r/cloudfoundry-incubator/executor?branch=HEAD)

![Executor Dreadnaught Class](http://img1.wikia.nocookie.net/__cb20130614094003/factpile/images/d/d9/Executor.jpg)

#Executor
it's time to play the game


## Usage
1) Start Warden-Linux locally with vagrant and virtualbox

```bash
git clone https://github.com/cloudfoundry-incubator/warden-linux
cd warden-linux

vagrant up
scripts/run-warden-remote-linux
# password: vagrant
```

1) Run Executor locally

```bash
git clone https://github.com/cloudfoundry-incubator/executor
cd executor

scripts/run-local
```
