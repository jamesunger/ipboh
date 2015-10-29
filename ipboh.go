package main

import (
	"fmt"
	"flag"
	"io/ioutil"
	"sync"
	"os"
	"bytes"
        coreunix "github.com/ipfs/go-ipfs/core/coreunix"
	pin "github.com/ipfs/go-ipfs/pin"
	core "github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/importer/chunk"
	importer "github.com/ipfs/go-ipfs/importer"
	corenet "github.com/ipfs/go-ipfs/core/corenet"
	peer "github.com/ipfs/go-ipfs/p2p/peer"
	fsrepo "github.com/ipfs/go-ipfs/repo/fsrepo"
	"encoding/json"
	//"github.com/ipfs/go-ipfs/blocks"
	dag "github.com/ipfs/go-ipfs/merkledag"

        bstore "github.com/ipfs/go-ipfs/blocks/blockstore"
        bserv "github.com/ipfs/go-ipfs/blockservice"
        syncds "github.com/ipfs/go-ipfs/Godeps/_workspace/src/github.com/jbenet/go-datastore/sync"
        ds "github.com/ipfs/go-ipfs/Godeps/_workspace/src/github.com/jbenet/go-datastore"
        offline "github.com/ipfs/go-ipfs/exchange/offline"

        ft "github.com/ipfs/go-ipfs/unixfs"


	"code.google.com/p/go.net/context"
	dagutils "github.com/ipfs/go-ipfs/merkledag/utils"
)

type Index struct {
	Entries []*Entry
}

type Entry struct {
	Name string
	Hash string
}

type Add struct {
	Name string
	Content []byte
}

func runIndex(n *core.IpfsNode, ctx context.Context, index *Index, wg *sync.WaitGroup) {
	list, err := corenet.Listen(n, "/pack/index")
	if err != nil {
		panic(err)
	}
	fmt.Printf("I have an index: %s\n", n.Identity)

	for {
		con, err := list.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		defer con.Close()

		fmt.Printf("Connection from: %s\n", con.Conn().RemotePeer())
		indexbytes,err := json.Marshal(index)
		if err != nil {
			panic(err)
		}
		count,err := con.Write(indexbytes)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println("Wrote bytes:",count)
		con.Close()
	}

	wg.Done()


}


func NewMemoryDagService() dag.DAGService {
        // build mem-datastore for editor's intermediary nodes
        bs := bstore.NewBlockstore(syncds.MutexWrap(ds.NewMapDatastore()))
        bsrv := bserv.New(bs, offline.Exchange(bs))
        return dag.NewDAGService(bsrv)
}


//var rootnode *dag.Node
func newDirNode() *dag.Node {
	return &dag.Node{Data: ft.FolderPBData()}
	/*if rootnode == nil {
		return &dag.Node{Data: ft.FolderPBData()}
	} else {
		return rootnode
	}*/
}



func runAdd(n *core.IpfsNode, ctx context.Context, index *Index, wg *sync.WaitGroup) {
	list, err := corenet.Listen(n, "/pack/add")
	if err != nil {
		panic(err)
	}
	fmt.Printf("I am ready to add: %s\n", n.Identity)

	for {
		con, err := list.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		defer con.Close()

		fmt.Printf("Connection from: %s\n", con.Conn().RemotePeer())
		readbytes,err := ioutil.ReadAll(con)
		// should not die here
		if err != nil {
			panic(err)
		}

		newadd := &Add{}
		err = json.Unmarshal(readbytes,newadd)
		// should not die here
		if err != nil {
			panic(err)
		}


		fmt.Println("They wanted to add:", newadd.Name)


		/*
		b := blocks.NewBlock(newadd.Content)
		fmt.Println("Block:", b.Key().B58String())
		
		blockadd,err := n.Blocks.AddBlock(b)
		if err != nil {
			panic(err)
		}
		fmt.Println(blockadd)
		*/


                //dagnode := &dag.Node{Data: newadd.Content}
                //dagadd, err := n.DAG.Add(dagnode)
                //if err != nil {
	//		panic(err)
         //       }



		newdirnode := newDirNode()
		e := dagutils.NewDagEditor(NewMemoryDagService(), newdirnode)


		reader := bytes.NewReader(newadd.Content)
		chnk, err := chunk.FromString(reader, "rabin")
		if err != nil {
			panic(err)
		}


		dagnode, err := importer.BuildDagFromReader(
                        n.DAG,
                        chnk,
                        importer.PinIndirectCB(n.Pinning.GetManual()),
                )

		err = e.InsertNodeAtPath(ctx,newadd.Name,dagnode,newDirNode)
		if err != nil {
			panic(err)
		}

                err = e.WriteOutputTo(n.DAG)
                if err != nil {
			panic(err)
                }



		key,err := dagnode.Key()
		if err != nil {
			panic(err)
		}
		fmt.Println("Added:", key.B58String())
		entry := Entry{Name: newadd.Name, Hash: key.B58String()}
		index.Entries = append(index.Entries,&entry)


		rootnd := e.GetNode()

		rnk, err := rootnd.Key()
		if err != nil {
			panic(err)
		}
                mp := n.Pinning.GetManual()
                mp.RemovePinWithMode(rnk, pin.Indirect)
                mp.PinWithMode(rnk, pin.Recursive)
                n.Pinning.Flush()

		fmt.Println("Pinned rn:", rnk)

	}

	wg.Done()

}




func getEntryList(n *core.IpfsNode, target peer.ID) *Index {


	index := makeIndex()
	con, err := corenet.Dial(n, target, "/pack/index")
	if err != nil {
		fmt.Println(err)
		return index
	}


//	rawbytes := make([]byte, 2048, 2048)
//	buf := bytes.NewBuffer(rawbytes)
//	_,err = io.Copy(buf, con)



	rawbytes,err := ioutil.ReadAll(con)
	if err != nil {
		fmt.Println(err)
		return index
	}
	//fmt.Println("Got result:" + string(rawbytes))


	err = json.Unmarshal(rawbytes,index)
	if err != nil {
		fmt.Println(err)
		return index
	}

	return index

}




func makeIndex() *Index {
	entries := make([]*Entry, 0)
	return &Index{ Entries: entries }
}


func getNewContent(name string) Add {


	rawbytes,err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}

	return Add{Name: name, Content: rawbytes}


}
			//newcontent := &Add{Name: "somenewcontent", Content: []byte("Some bogus content3\n")}

func hasCmd(cmdname string) bool {
	for i := range os.Args {
		if os.Args[i] == cmdname {
			return true
		}
	}
	return false
}

func getCmdArg(cmdname string) string {
	for i := range os.Args {
		if os.Args[i] == cmdname {
			return os.Args[i+1]
		}
	}
	return ""

}

func main() {
	// Basic ipfsnode setup
	//r,_ := fsrepo.Open("~/.ipfs")
	r, err := fsrepo.Open("~/.ipfs")
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n, err := core.NewNode(ctx, &core.BuildCfg{Online: true, Repo: r})
	if err != nil {
		panic(err)
	}

	//fmt.Println(n)
	var wg sync.WaitGroup
	wg.Add(2)

	index := makeIndex()

	var server,client bool
	var serverhash,add string
	var dumphash string
	flag.BoolVar(&server, "s", false, "Run as server")
	flag.BoolVar(&client, "c", false, "Run as client")
	flag.StringVar(&dumphash, "d", "", "Dump contents of hash")
	flag.StringVar(&add, "a", "", "Add content")
	flag.StringVar(&serverhash, "h", "", "Server hash to connect to")
	flag.Parse()


	server = hasCmd("server")
	client = hasCmd("client")
	if hasCmd("add") {
		add = getCmdArg("add")
	}

	if hasCmd("cat") {
		dumphash = getCmdArg("cat")
	}



	if server {
		go runIndex(n,ctx,index,&wg)
		go runAdd(n,ctx,index,&wg)
		wg.Wait()
	} else {
		//fmt.Println("I'll start up as a client connecting to:", serverhash)

		target, err := peer.IDB58Decode(serverhash)
		if err != nil {
			panic(err)
		}

		if add != "" {
			con, err := corenet.Dial(n, target, "/pack/add")
			if err != nil {
				fmt.Println(err)
				return
			}

			newcontent := getNewContent(add)
			contentbytes,err := json.Marshal(newcontent)
			if err != nil {
				fmt.Println(err)
				return
			}
			_,err = con.Write(contentbytes)
			if err != nil {
				panic(err)
			}
			con.Close()
			//time.Sleep(15*time.Second)
		} else if dumphash != "" {
			// FIXME: validate this in case there is a 46 len name!
			foundhash := false
			if len(dumphash) != 46 {
				entrylist := getEntryList(n,target)
				//fmt.Println(entrylist)
				for i := range entrylist.Entries {
					if entrylist.Entries[i].Name == dumphash {
						dumphash = entrylist.Entries[i].Hash
						foundhash = true
						break
					}
				}
			}

			if !foundhash {
				fmt.Println("No entry found.")
				return
			}

			reader,err := coreunix.Cat(ctx, n, dumphash)
			if err != nil {
				panic(err)
			}

			bytes,err := ioutil.ReadAll(reader)
			if err != nil {
				panic(err)
			}

			fmt.Println(string(bytes))
		} else {


			entrylist := getEntryList(n,target)
			for i := range entrylist.Entries {
				fmt.Println(entrylist.Entries[i].Name)
			}

			/*
			con, err := corenet.Dial(n, target, "/pack/index")
    			if err != nil {
        			fmt.Println(err)
        			return
    			}

    			io.Copy(os.Stdout, con)
			*/
		}
	}


}

