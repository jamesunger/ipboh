
IPFS Bag of Holding
===================

This is a POC to play around with IPFS. It runs a server on one host which
clients can directly query using the IPFS swarm dial features. Files are then
fetched via the IPFS network. There is nothing here that IPFS doesn't do with
the ipfs command, but ipboh simplifies the interface with a flat list of files,
is easy to backup/move around on the serverside and supports PGP.

Example
-------
To run the server, somewhere:
```
 ipboh server
```

If run for the first time, it will print out the IPFS ID which is needed by the client. Data by default with be persisted in /tmp/ipboh-data and can be overridden with the -d flag. Hosting behind a NAT is currently problematic from what I've noticed.

Then ipboh may be used as a client on another host to list entries:
```
 ipboh -h HASHOFSERVERNODE
```

*Please note* that ipboh will spawn a separate process into the background to field client requests over a local HTTP server. Earlier versions did everything in one process which really isn't very efficient for IPFS since it is a persistent P2P network. The server nukes itself after half an hour with requests. This is so I can feel safe running this randomly on whatever box I find myself on without worry of leaving a daemon behind.

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

If you add content with the same name it will be appended to list but by
default duplicates will be omitted from the entry list in favor of the most
recently added entry. This means you can keep adding content with the same name
and ensure clients will always get the latest. If you want a previous version
use verbose:
```
$ ipboh -v
```
to list duplicate entries. To fetch an entry that is not the most recent
reference the hash instead of the name (or just use ipfs command):
```
$ ipboh cat QmdrqwWC74ANFDJqAmEL3BQEbw5apg6Kra1TDL1B25JTPG
```

PGP Support
-----------
ipboh will use PGP keys if already present in ~/.gnupg. If you want to encrypt
something, just specify the PGP key as such: 
```
$ echo "encrypted content" | ipboh -e testkey add enccontent
```
when fetched, it will attempt to decrypt automatically:
```
$ ipboh cat enccontent
Password: 
encrypted content
```
