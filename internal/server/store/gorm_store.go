package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"itagent/internal/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *GormStore) RegisterDevice(ctx context.Context, d Device) (string, error) {
	if d.DeviceID == "" {
		return "", fmt.Errorf("device_id required")
	}

	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	now := time.Now().UTC()

	agentDev := model.AgentDevice{
		DeviceID:     d.DeviceID,
		DeviceToken:  token,
		Hostname:     d.Hostname,
		OS:           d.OS,
		AgentVersion: d.AgentVersion,
		LastSeenAt:   now,
		RegisteredAt: now,
	}

	// Insert or ignore
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "device_id"}},
		DoNothing: true,
	}).Create(&agentDev).Error

	if err != nil {
		return "", err
	}

	// If it was ignored, we don't return the new token, we should fetch the existing one if we needed to,
	// but the original logic just tried to insert and returned the generated token?
	// Wait, original logic:
	// INSERT INTO devices ... ON CONFLICT DO NOTHING
	// It returned the token it generated, but if it was a conflict, it wouldn't update the token in DB, so the returned token would be WRONG.
	// Actually, wait, let's just return the existing token if it exists.
	var existing model.AgentDevice
	if err := s.db.WithContext(ctx).Where("device_id = ?", d.DeviceID).First(&existing).Error; err != nil {
		return "", err
	}
	return existing.DeviceToken, nil
}

func (s *GormStore) Authenticate(ctx context.Context, deviceID, token string) error {
	var dev model.AgentDevice
	if err := s.db.WithContext(ctx).Where("device_id = ?", deviceID).First(&dev).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUnauthorized
		}
		return err
	}
	if dev.DeviceToken != token {
		return ErrUnauthorized
	}
	return nil
}

func (s *GormStore) UpsertDeviceSeen(ctx context.Context, d Device) error {
	return s.db.WithContext(ctx).Model(&model.AgentDevice{}).
		Where("device_id = ?", d.DeviceID).
		Updates(map[string]interface{}{
			"hostname":      d.Hostname,
			"os":            d.OS,
			"agent_version": d.AgentVersion,
			"last_seen_at":  time.Now().UTC(),
		}).Error
}

func (s *GormStore) SaveReport(ctx context.Context, r Report) (int64, error) {
	agentRep := model.AgentReport{
		DeviceID:   r.DeviceID,
		ReportType: r.ReportType,
		Payload:    r.Payload,
		ReportedAt: r.ReportedAt.UTC(),
		ReceivedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&agentRep).Error; err != nil {
		return 0, err
	}
	return agentRep.ID, nil
}

func mapDevice(d model.AgentDevice) Device {
	return Device{
		DeviceID:     d.DeviceID,
		DeviceToken:  d.DeviceToken,
		Hostname:     d.Hostname,
		OS:           d.OS,
		AgentVersion: d.AgentVersion,
		LastSeenAt:   d.LastSeenAt,
		RegisteredAt: d.RegisteredAt,
	}
}

func (s *GormStore) GetDevice(ctx context.Context, deviceID string) (Device, error) {
	var dev model.AgentDevice
	if err := s.db.WithContext(ctx).Where("device_id = ?", deviceID).First(&dev).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Device{}, ErrNotFound
		}
		return Device{}, err
	}
	return mapDevice(dev), nil
}

func (s *GormStore) ListDevices(ctx context.Context, limit, offset int) ([]Device, error) {
	var devs []model.AgentDevice
	if err := s.db.WithContext(ctx).Order("registered_at asc").Limit(limit).Offset(offset).Find(&devs).Error; err != nil {
		return nil, err
	}
	out := make([]Device, len(devs))
	for i, d := range devs {
		out[i] = mapDevice(d)
	}
	return out, nil
}

func (s *GormStore) ListReports(ctx context.Context, deviceID string, limit, offset int) ([]Report, error) {
	var reps []model.AgentReport
	if err := s.db.WithContext(ctx).Where("device_id = ?", deviceID).Order("id desc").Limit(limit).Offset(offset).Find(&reps).Error; err != nil {
		return nil, err
	}
	out := make([]Report, len(reps))
	for i, r := range reps {
		out[i] = Report{
			ID:         r.ID,
			DeviceID:   r.DeviceID,
			ReportType: r.ReportType,
			Payload:    r.Payload,
			ReportedAt: r.ReportedAt,
			ReceivedAt: r.ReceivedAt,
		}
	}
	return out, nil
}

func (s *GormStore) GetSnapshot(ctx context.Context, deviceID string) (Snapshot, error) {
	var snap model.AgentSnapshot
	if err := s.db.WithContext(ctx).Where("device_id = ?", deviceID).First(&snap).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Snapshot{}, ErrNotFound
		}
		return Snapshot{}, err
	}
	return Snapshot{
		DeviceID:  snap.DeviceID,
		Payload:   snap.Payload,
		UpdatedAt: snap.UpdatedAt,
	}, nil
}

func (s *GormStore) SaveSnapshot(ctx context.Context, snap Snapshot) error {
	agentSnap := model.AgentSnapshot{
		DeviceID:  snap.DeviceID,
		Payload:   snap.Payload,
		UpdatedAt: time.Now().UTC(),
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "device_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"payload", "updated_at"}),
	}).Create(&agentSnap).Error
}

func (s *GormStore) SaveChangeEvent(ctx context.Context, e ChangeEvent) (int64, error) {
	detail, _ := json.Marshal(e.Detail)
	event := model.AgentChangeEvent{
		DeviceID:  e.DeviceID,
		Kind:      e.Kind,
		Severity:  e.Severity,
		Message:   e.Message,
		Detail:    detail,
		Acked:     false,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&event).Error; err != nil {
		return 0, err
	}
	return event.ID, nil
}

func (s *GormStore) ListChangeEvents(ctx context.Context, includeAcked bool, limit, offset int) ([]ChangeEvent, error) {
	var events []model.AgentChangeEvent
	q := s.db.WithContext(ctx)
	if !includeAcked {
		q = q.Where("acked = ?", false)
	}
	if err := q.Order("id desc").Limit(limit).Offset(offset).Find(&events).Error; err != nil {
		return nil, err
	}
	out := make([]ChangeEvent, len(events))
	for i, e := range events {
		var detail map[string]string
		if len(e.Detail) > 0 {
			_ = json.Unmarshal(e.Detail, &detail)
		}
		out[i] = ChangeEvent{
			ID:        e.ID,
			DeviceID:  e.DeviceID,
			Kind:      e.Kind,
			Severity:  e.Severity,
			Message:   e.Message,
			Detail:    detail,
			Acked:     e.Acked,
			CreatedAt: e.CreatedAt,
		}
	}
	return out, nil
}

func (s *GormStore) AckChangeEvent(ctx context.Context, id int64) error {
	res := s.db.WithContext(ctx).Model(&model.AgentChangeEvent{}).Where("id = ?", id).Update("acked", true)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
