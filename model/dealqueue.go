package model

import (
	"time"

	"github.com/application-research/estuary/util"
	"gorm.io/gorm"
)

type DealQueue struct {
	gorm.Model
	ID                     uint64     `gorm:"primarykey" json:"-"`
	UserID                 uint       `gorm:"index:user_id_cont_id;index;not null" json:"-"`
	ContID                 uint64     `gorm:"unique;index:user_id_cont_id;index;not null" json:"-"`
	ContCID                util.DbCID `gorm:"index;not null" json:"-"`
	CommpDone              bool       `gorm:"index:commp_done_commp_attempted_commp_next_attempt_at;index:can_deal_commp_done_deal_next_attempt_at;index:can_deal_commp_done_deal_check_next_attempt_at;not null" json:"-"`
	CommpAttempted         uint       `gorm:"index:commp_done_commp_attempted_commp_next_attempt_at;not null" json:"-"`
	CommpNextAttemptAt     time.Time  `gorm:"index:commp_done_commp_attempted_commp_next_attempt_at;" json:"-"`
	CanDeal                bool       `gorm:"index:can_deal_commp_done_deal_next_attempt_at;index:can_deal_commp_done_deal_check_next_attempt_atnot null" json:"-"`
	DealCount              int        `gorm:"index;not null" json:"-"`
	DealCheckNextAttemptAt time.Time  `gorm:"index:can_deal_commp_done_deal_next_attempt_at;index:can_deal_commp_done_deal_check_next_attempt_at" json:"-"`
	DealNextAttemptAt      time.Time  `gorm:"index" json:"-"`
}
