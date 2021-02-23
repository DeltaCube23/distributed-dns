package main

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/krithikvaidya/distributed-dns/replicated_kv_store/protos"
)

type testing_st struct {
	mu          sync.Mutex           // The mutex for performing operations on the struct
	t           *testing.T           // The testing object for utility funcs to display errors
	n           int                  // The number of nodes in the system
	rep_addrs   []string             // The addresses of the replicas in the system
	nodes       []*RaftNode          // The RaftNode objects of the individual replicas
	active      []bool               // The status of each node, whether it is active(true) or not(false)
	ctx_list    []context.Context    // The list of contexts of all the raft nodes
	cancel_list []context.CancelFunc // The list of cancel functions for the contexts
	start       time.Time            // Time at which make_testing_st() was called
}

/*
 * This function is initially called in a test case for
 * setting up a system on which raft can be run.
 *
 * It initializes a struct for managing the current
 * testable raft system.
 *
 * The system is created by calling two functions,
 * `setup_raft_node()` for initial creation of the
 * `RaftNode` structs and `connect_raft_nodes()` to
 * start the required services and connect the nodes
 * together.
 */
func start_test(t *testing.T, n int) *testing_st {
	new_test_st := &testing_st{}
	new_test_st.t = t
	new_test_st.n = n
	new_test_st.nodes = make([]*RaftNode, n)
	new_test_st.active = make([]bool, n)
	new_test_st.rep_addrs = make([]string, n)
	new_test_st.ctx_list = make([]context.Context, n)
	new_test_st.cancel_list = make([]context.CancelFunc, n)

	// Create the replicas
	for i := 0; i < n; i++ {

		new_test_st.nodes[i] = setup_raft_node(new_test_st.ctx_list[i], i, new_test_st.n, true)
		new_test_st.rep_addrs[i] = ":500" + strconv.Itoa(i)
	}

	// Connect the replicas together to setup the system
	for i := 0; i < n; i++ {

		// Create the context for the current node
		new_test_st.ctx_list[i], new_test_st.cancel_list[i] = context.WithCancel(context.Background())

		go new_test_st.nodes[i].connect_raft_node(new_test_st.ctx_list[i], i, new_test_st.rep_addrs, true)

		// Set the current node as active
		new_test_st.active[i] = true
	}

	return new_test_st
}

/*
 * This function is used to end a test case.
 *
 * This is done by bringing down all nodes in the current testing
 * system by canceling the contexts of all of the nodes.
 *
 * Needs to be called at the end of every test.
 */
func end_test(test_st *testing_st) {
	for i := 0; i < test_st.n; i++ {
		test_st.cancel_list[i]()
	}
}

/*
 * This function is used to simulate the crash of a node
 * in the current system by disconnecting it with other
 * nodes and then killing the various services on the
 * node.
 */
func (test_st *testing_st) crash_raft_node(id int) {
	// Save the current status in persistent storage
	test_st.nodes[id].persistToStorage()

	// Mark the node as 'inactive'
	test_st.active[id] = false

	// Cancel the context of the node to be crashed
	test_st.cancel_list[id]()
}

/*
 * This function can be called to find the number of
 * active nodes with the states as 'Leader'
 */
func (test_st *testing_st) count_leader() int {
	n_leaders := 0
	for i := 0; i < test_st.n; i++ {
		if test_st.active[i] {
			if test_st.nodes[i].state == Leader {
				n_leaders++
			}
		}
	}

	return n_leaders
}

/*
 * This function is used to find the replica ID of the
 * cuurent leader node in the system.
 */
func (test_st *testing_st) find_leader() int {
	leader_id := -1
	for i := 0; i < test_st.n; i++ {
		if test_st.active[i] {
			if test_st.nodes[i].state == Leader {
				leader_id = i
			}
		}
	}
	return leader_id
}

/*
 * This function checks whether the terms on the active
 * nodes are same.
 */
func (test_st *testing_st) check_terms() bool {
	var sys_term int32
	sys_term = -1
	for i := 0; i < test_st.n; i++ {
		if test_st.active[i] {
			if sys_term == -1 {
				sys_term = test_st.nodes[i].currentTerm
			} else if sys_term != test_st.nodes[i].currentTerm {
				return false
			}
		}
	}

	return true
}

/*
 * This function checks if, at a particular index, the majority of
 * the nodes have the same log entry.
 */
func (test_st *testing_st) check_consensus(index int) int {
	count := 0

	var logs []protos.LogEntry

	// Obtain the log values for the given index from all the currently active
	// nodes
	for i := 0; i < test_st.n; i++ {
		if test_st.active[i] == true {
			logs = append(logs, test_st.nodes[i].log[index])
		}
	}

	// Check the most frequent element of the array
	for i := 0; i < test_st.n; i++ {
		curr_count := 1
		for j := 0; j < test_st.n; j++ {
			// Ignore the current index
			if i == j {
				continue
			}

			// Check the entry at the current index against the other indexes
			if logs[i].Term == logs[j].Term &&
				len(logs[i].Operation) == len(logs[j].Operation) &&
				logs[i].Clientid == logs[j].Clientid {
				length := len(logs[i].Operation)
				agree := true
				for k := 0; k < length; k++ {
					if logs[i].Operation[k] != logs[j].Operation[k] {
						agree = false
					}
				}
				if agree == true {
					curr_count++
				}
			}
		}

		// Taking only the max value
		if curr_count > count {
			count = curr_count
		}
	}

	return count
}
