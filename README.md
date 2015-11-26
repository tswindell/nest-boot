# nest-boot
The Linux Namespace based Nest boot loader.

## Introduction
The aim of this project is to create an easy to use tool, to instantiate a Linux Namespace (A.K.A. Container). Configuration is parsed from a JSON object, and used to tag the instance, setup networking, mounting filesystems, etc.

## Installing
To install this project:
```
$ go get github.com/tswindell/nest-boot
$ go get github.com/tswindell/nest-boot/network-helper
$ sudo chown root.root $GOPATH/bin/network-helper && sudo chmod +s $GOPATH/bin/network-helper
```

## Example Usage
To create a basic environment with isolated network:
```
$ sudo brctl addbr vbr0
$ sudo ifconfig vbr0 10.0.0.1
$ nest-boot --network-helper=$GOPATH/bin/network-helper /bin/bash
```
This should execute Bash in a new environment, where you can then execute:
```
$ ifconfig veth0 10.0.0.2
$ ping 10.0.0.1
```
