package repository

// PreparedMediaDescription 保留生成报告时读取的简介字段。
type PreparedMediaDescription struct {
	Statement string
	Body      string
}

// UpdatePreparedMediaInfo 在媒体文本及状态未变化时按原草稿主键保存报告。
// 从简介提取的报告还要求简介与提取时一致，避免回填过时的引用内容。
func (r *MigrateRepository) UpdatePreparedMediaInfo(hash, torrentID, siteName, previousReport, previousStatus, report, updatedAt string, description *PreparedMediaDescription) (bool, error) {
	query := r.store.DB.Table("seed_parameters").
		Where("hash = ? AND torrent_id = ? AND site_name = ?", hash, torrentID, siteName).
		Where("COALESCE(mediainfo, '') = ? AND COALESCE(mediainfo_status, '') = ?", previousReport, previousStatus)
	if description != nil {
		query = query.Where("COALESCE(statement, '') = ? AND COALESCE(body, '') = ?", description.Statement, description.Body)
	}
	result := query.Updates(map[string]any{
		"mediainfo": report, "mediainfo_status": "completed", "bdinfo_error": "", "updated_at": updatedAt,
	})
	return result.RowsAffected == 1, result.Error
}
