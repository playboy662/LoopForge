package repo

import (
	"os"
	"testing"
)

func TestJenkinsReceiptSurvivesRepeatedAttempts(t *testing.T) {
	store, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	checkJenkinsReceipt(t, store)
}

func TestJenkinsReceiptOnMySQL(t *testing.T) {
	dsn := os.Getenv("AGENTFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("AGENTFLOW_TEST_MYSQL_DSN is not set")
	}
	store, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	checkJenkinsReceipt(t, store)
}

func checkJenkinsReceipt(t *testing.T, store *Store) {
	t.Helper()
	key := JenkinsReceipt{RunID: "jenkins-test-" + t.Name(), NodeID: "deploy", Attempt: 1}
	t.Cleanup(func() {
		store.DB.Where("run_id = ? AND node_id = ?", key.RunID, key.NodeID).Delete(&JenkinsReceipt{})
	})
	receipt, fresh, err := store.ReserveJenkinsReceipt(key)
	if err != nil || !fresh {
		t.Fatalf("reserve: %v %v", fresh, err)
	}
	key.Attempt = 2
	if pending, fresh, err := store.ReserveJenkinsReceipt(key); err != nil || fresh || pending.State != JenkinsPending {
		t.Fatalf("pending: %+v %v %v", pending, fresh, err)
	}
	receipt.State = JenkinsRejected
	if err := store.SaveJenkinsReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	receipt, fresh, err = store.ReserveJenkinsReceipt(key)
	if err != nil || !fresh || receipt.State != JenkinsPending {
		t.Fatalf("retry rejected request: %+v %v %v", receipt, fresh, err)
	}
	receipt.URL = "https://jenkins.example/queue/item/42/"
	receipt.State, receipt.Queued = JenkinsAccepted, true
	if err := store.SaveJenkinsReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	if accepted, fresh, err := store.ReserveJenkinsReceipt(key); err != nil || fresh || accepted.URL != receipt.URL || accepted.State != JenkinsAccepted || !accepted.Queued {
		t.Fatalf("accepted: %+v %v %v", accepted, fresh, err)
	}
}
