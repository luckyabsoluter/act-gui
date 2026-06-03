package main

import "gorm.io/gorm"

func terminalStatus(status string) bool {
	switch status {
	case "success", "failure", "cancelled", "skipped":
		return true
	default:
		return false
	}
}

func normalizeFinishStatus(status string) string {
	switch status {
	case "success", "failure", "cancelled", "skipped":
		return status
	default:
		return "success"
	}
}

func finishRun(db *gorm.DB, runID uint, requestedStatus string) string {
	if runID == 0 {
		return normalizeFinishStatus(requestedStatus)
	}

	status := normalizeFinishStatus(requestedStatus)
	_ = db.Transaction(func(tx *gorm.DB) error {
		var run Run
		if err := tx.First(&run, runID).Error; err != nil {
			return err
		}

		if terminalStatus(run.Status) {
			status = normalizeFinishStatus(run.Status)
		} else if err := tx.Model(&Run{}).Where("id = ?", runID).Update("status", status).Error; err != nil {
			return err
		}

		waitingStatus := "skipped"
		if status == "cancelled" {
			waitingStatus = "cancelled"
		}

		if err := tx.Model(&Job{}).
			Where("run_id = ? AND (status = ? OR status = ?)", runID, "running", "").
			Update("status", status).Error; err != nil {
			return err
		}
		if err := tx.Model(&Job{}).
			Where("run_id = ? AND status = ?", runID, "waiting").
			Update("status", waitingStatus).Error; err != nil {
			return err
		}

		if err := tx.Model(&Step{}).
			Where("job_id IN (?) AND (status = ? OR status = ?)", tx.Model(&Job{}).Select("id").Where("run_id = ?", runID), "running", "").
			Update("status", status).Error; err != nil {
			return err
		}
		if err := tx.Model(&Step{}).
			Where("job_id IN (?) AND status = ?", tx.Model(&Job{}).Select("id").Where("run_id = ?", runID), "waiting").
			Update("status", waitingStatus).Error; err != nil {
			return err
		}

		return nil
	})
	return status
}

func runHasTerminalStatus(db *gorm.DB, runID uint) bool {
	var run Run
	if err := db.Select("status").First(&run, runID).Error; err != nil {
		return false
	}
	return terminalStatus(run.Status)
}
