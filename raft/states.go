package raft

import (
	"context"
	"log"

	"github.com/krithikvaidya/distributed-dns/raft/protos"
)

// Method to transition the replica to Follower state.
func (node *RaftNode) ToFollower(ctx context.Context, term int32) {

	log.Printf("\nIn ToFollower, previous state: %v\n", node.state)
	prevState := node.state
	node.state = Follower
	node.currentTerm = term
	node.votedFor = -1

	node.persistToStorage()

	// If node was a leader, start election timer. Else if it was a follower or
	// candidate, reset the election timer.

	if prevState == Leader {
		go node.RunElectionTimer(ctx)
	} else {
		go func() {
			node.electionResetEvent <- true
		}()
	}

	log.Printf("\nFinished ToFollower\n")
}

// ToCandidate is called when election timer runs out
// without heartbeat from leader
func (node *RaftNode) ToCandidate(ctx context.Context) {

	node.state = Candidate
	node.currentTerm++
	node.votedFor = node.Meta.replica_id
	node.persistToStorage()
	// We can start an election for the candidate to become the leader
	node.StartElection(ctx)
}

// ToLeader is called when the candidate gets majority votes in election
func (node *RaftNode) ToLeader(ctx context.Context) {

	log.Printf("\nTransitioning to leader\n")

	// Stop election timer since leader doesn't need it
	node.stopElectiontimer <- true

	node.state = Leader

	node.nextIndex = make([]int32, node.Meta.n_replicas, node.Meta.n_replicas)
	node.matchIndex = make([]int32, node.Meta.n_replicas, node.Meta.n_replicas)

	// Initialize nextIndex, matchIndex
	for replica_id := int32(0); replica_id < node.Meta.n_replicas; replica_id++ {

		if int32(replica_id) == node.Meta.replica_id {
			continue
		}

		node.nextIndex[replica_id] = int32(len(node.log))
		node.matchIndex[replica_id] = int32(0)

	}

	// Send no-op for synchronization
	var operation []string
	operation = append(operation, "NO-OP")

	node.log = append(node.log, protos.LogEntry{Term: node.currentTerm, Operation: operation, Clientid: " "})

	msg := &protos.AppendEntriesMessage{

		Term:         node.currentTerm,
		LeaderId:     node.Meta.replica_id,
		LeaderCommit: node.commitIndex,
		LeaderAddr:   node.Meta.nodeAddress,
		LatestClient: node.Meta.latestClient,
	}

	node.persistToStorage()
	node.raft_node_mutex.Unlock()

	// If replicating NO-OP fails, keep retrying while the replica thinks it's still a leader.
	for {

		success := make(chan bool)
		node.LeaderSendAEs("NO-OP", msg, int32(len(node.log)-1), success)

		if <-success {

			node.raft_node_mutex.Lock()
			node.commitIndex++
			node.persistToStorage()
			node.raft_node_mutex.Unlock()
			node.commits_ready <- 1
			break

		} else {

			node.raft_node_mutex.RLock()

			if node.state != Leader {
				log.Printf("\nStopped attempting transition to leader\n")
				node.raft_node_mutex.RUnlock()
				return
			}

			node.raft_node_mutex.RUnlock()
		}

	}

	go node.HeartBeats(ctx)
	
	log.Printf("\nTransitioned to leader\n")

}
