
IPFS Bag of Holding
===================

This is a POC to play around with IPFS. It runs a server on one host which clients can directly query using the IPFS swarm dial features. Files are then fetched via the IPFS network. There is nothing here that IPFS doesn't do with the ipfs command, but ipboh simplifies the interface and functionlity and supports OpenPGP.

To run the server, somewhere:
```
 ipboh server
```

Data by default with be persisted in /tmp/ipboh-data and can be overridden with the -d flag. Hosting behind a NAT is currently problematic from what I've noticed.

Then ipboh may be used as a client on another host to list entries:
```
 ipboh -h HASHOFSERVERNODE
```

*Please note* that ipboh will spawn a separate process into the background to field client requests over a local HTTP server. Earlier versions did everything in one process which really isn't very efficient for IPFS since it is a persistent P2P network.

Nothing will list on a new node.

The '-h' option is only necessary once. Once -h has been set ipboh saves it in: ~/.ipbohrc.

To add some entries:
```
 echo "some content" | ipboh add testcontent1
 cat /tmp/largerfile.data | ipboh add largerfile.data
```

Listing entries again:
```
$ ipboh
Qmb1EXrDyKhNWfvLPYK4do3M9nU7BuLAcbqBir6aUrDsRY testcontent1
QmWuXZhpYEXkB1j7N45SigyjSgQf5GrY1mgBFwEu4ChjTB largerfile.data
```

and to get an entry:
```
$ ipboh cat testcontent1
some content
```

