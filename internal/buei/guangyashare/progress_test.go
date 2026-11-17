package guangyashare

import "testing"

func TestTransferProgressTracker(t *testing.T) {
	tracker := newTransferProgressTracker()
	if _, ok := tracker.get("missing"); ok {
		t.Fatal("unknown progress id should not be found")
	}

	tracker.start("p1")
	entry, ok := tracker.get("p1")
	if !ok || entry.Phase != GuangyaSharePhaseTransfer {
		t.Fatalf("unexpected initial progress: %+v ok=%v", entry, ok)
	}

	tracker.setPhase("p1", GuangyaSharePhaseRename)
	if entry, _ = tracker.get("p1"); entry.Phase != GuangyaSharePhaseRename {
		t.Fatalf("phase was not updated: %+v", entry)
	}

	tracker.setPhase("p1", GuangyaSharePhaseStrm)
	tracker.finish("p1", "")
	if entry, _ = tracker.get("p1"); entry.Phase != GuangyaSharePhaseDone || entry.Error != "" {
		t.Fatalf("unexpected finished progress: %+v", entry)
	}

	tracker.start("p2")
	tracker.finish("p2", "转存失败")
	if entry, _ = tracker.get("p2"); entry.Phase != GuangyaSharePhaseFailed || entry.Error != "转存失败" {
		t.Fatalf("unexpected failed progress: %+v", entry)
	}

	// 未登记过的 id 更新阶段不应创建条目。
	tracker.setPhase("p3", GuangyaSharePhaseRename)
	if _, ok := tracker.get("p3"); ok {
		t.Fatal("setPhase should not create progress entries")
	}
}

func TestServiceTransferProgressWithoutTracker(t *testing.T) {
	svc := &Service{}
	if _, ok := svc.TransferProgress("p1"); ok {
		t.Fatal("service without tracker should report no progress")
	}
	var nilSvc *Service
	if _, ok := nilSvc.TransferProgress("p1"); ok {
		t.Fatal("nil service should report no progress")
	}
}
