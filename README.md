
IPFS Bag of Holding
===================

This is a POC to play around with IPFS. It runs a server in memory on one node and clients connect to list file entries and download them.

I wanted to learn IPFS and have always wanted a simple tool that works anywhere to efficiently save/store arbitrary files with PGP support.


To run as a server:
```
 ipboh server
```

Data by default with be persisted in /tmp/ipboh-data and can be overridden with the -d flag.

Then ipboh may be used as a client on another host to list entries:
```
 ipboh -h HASHOFSERVERNODE
```

Nothing will show

Once -h has been set ipboh saves that node in ~/.ipbohrc so providing the argument afterwords is unnecessary. To add an entry:

 echo "some content" | ipboh add ENTRYNAME
```

and to get an entry:
```
 ipboh -h HASHOFSERVERNODE cat ENTRYNAME
```

The 


