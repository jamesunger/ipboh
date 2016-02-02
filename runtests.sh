#!/bin/bash

rm -rf test/.ipfs
rm test/ipboh-index.txt
rm test/.ipbohrc
echo "hi" | GO15VENDOREXPERIMENT=0 TESTTARGET=$1 go test -v -cover -coverprofile cover.out


