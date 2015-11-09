
IPFS Bag of Holding
===================

This is a POC to play around with IPFS. It runs a server in memory on one node and clients connect to list file entries and download them.

I wanted to learn IPFS and have always wanted a simple tool that works anywhere to efficiently save/store arbitrary files with PGP support.

The goal of this project is a convenient (read: download, use) tool that gives trivial shell integrated access to arbitrary files on arbitrary boxes.


To run the server, somewhere:
```
 ipboh server
```

Data by default with be persisted in /tmp/ipboh-data and can be overridden with the -d flag. Hosting behind a NAT is currently problematic from what I've noticed.

Then ipboh may be used as a client on another host to list entries:
```
 ipboh -h HASHOFSERVERNODE
```

*Please note* that ipboh will spawn a separate process into the background to field requests over a local HTTP server. Earlier versions did everything in one process which really isn't a good idea for IPFS since it is a persistent P2P network.

Nothing will list on a new node.

The '-h' option is only necessary once. Once -h has been set ipboh saves it in: ~/.ipbohrc.

To add an entry:

 echo "some content" | ipboh add ENTRYNAME
```

and to get an entry:
```
 ipboh cat ENTRYNAME
```

