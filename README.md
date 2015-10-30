
IPFS Bag of Holding
===================

This is a POC to play around with IPFS. It runs a server in memory on one node and clients connect to list file entries and download them.

To run as a server:
```
 ipfs init
 ipboh -s
```

Then as a client to list entries:
```
 ipfs init
 ipboh -h HASHOFSERVERNODE
```

and to add an entry:
```
 echo "some content" | ipboh -h HASHOFSERVERNODE add ENTRYNAME
```

and to get an entry:
```
 ipboh -h HASHOFSERVERNODE cat ENTRYNAME
```


