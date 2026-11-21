package repository

// UpdatePreparedMediaInfo 在媒体文本及状态未变化时按原草稿主键保存报告。
// 参数/返回：传入草稿键、此前读取的原始媒体文本与状态、本次报告及保存时间。返回是否写入。
// 失败场景：记录或字段已变化不写入；数据库错误原样返回。只更新媒体信息相关字段。
func (r *MigrateRepository) UpdatePreparedMediaInfo(hash, torrentID, siteName, previousReport, previousStatus, report, updatedAt string) (bool, error) {
	result := r.store.DB.Table("seed_parameters").
		Where("hash = ? AND torrent_id = ? AND site_name = ?", hash, torrentID, siteName).
		Where("COALESCE(mediainfo, '') = ? AND COALESCE(mediainfo_status, '') = ?", previousReport, previousStatus).
		Updates(map[string]any{
			"mediainfo": report, "mediainfo_status": "completed", "bdinfo_error": "", "updated_at": updatedAt,
		})
	return result.RowsAffected == 1, result.Error
}
