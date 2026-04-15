// internal/db/models.go

package db

import "time"

type User struct {
	ID        string    `json:"id" db:"id"`
	Email     string    `json:"email" db:"email"`
	Name      string    `json:"name" db:"name"`
	Password  string    `json:"-" db:"password_hash"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

type List struct {
	ID         string    `json:"id" db:"id"`
	Name       string    `json:"name" db:"name"`
	InviteCode string    `json:"inviteCode" db:"invite_code"`
	CreatedAt  time.Time `json:"createdAt" db:"created_at"`
}

type ListMember struct {
	ListID string    `json:"listId" db:"list_id"`
	UserID string    `json:"userId" db:"user_id"`
	JoinedAt time.Time `json:"joinedAt" db:"joined_at"`
}

type Item struct {
	ID        string     `json:"id" db:"id"`
	ListID    string     `json:"listId" db:"list_id"`
	Name      string     `json:"name" db:"name"`
	Quantity  float64    `json:"quantity" db:"quantity"`
	Unit      string     `json:"unit" db:"unit"`
	Checked   bool       `json:"checked" db:"checked"`
	CheckedBy *string    `json:"checkedBy,omitempty" db:"checked_by"`
	CheckedAt *time.Time `json:"checkedAt,omitempty" db:"checked_at"`
	AddedBy   string     `json:"addedBy" db:"added_by"`
	CreatedAt time.Time  `json:"createdAt" db:"created_at"`
}

type Store struct {
	ID           string             `json:"id" db:"id"`
	ListID       string             `json:"listId" db:"list_id"`
	Name         string             `json:"name" db:"name"`
	TripCount    int                `json:"tripCount" db:"trip_count"`
	LearnedOrder map[string]float64 `json:"learnedOrder" db:"-"` // populated separately
	CreatedAt    time.Time          `json:"createdAt" db:"created_at"`
}

type StoreOrderEntry struct {
	StoreID  string  `json:"storeId" db:"store_id"`
	ItemName string  `json:"itemName" db:"item_name"`
	AvgPos   float64 `json:"avgPos" db:"avg_pos"`
	Samples  int     `json:"samples" db:"samples"`
}

type Trip struct {
	ID          string      `json:"id" db:"id"`
	ListID      string      `json:"listId" db:"list_id"`
	StoreID     *string     `json:"storeId,omitempty" db:"store_id"`
	StoreName   *string     `json:"storeName,omitempty" db:"store_name"`
	CompletedBy string      `json:"completedBy" db:"completed_by"`
	CompletedAt time.Time   `json:"completedAt" db:"completed_at"`
	ItemCount   int         `json:"itemCount" db:"item_count"`
	ReceiptURL  *string     `json:"receiptUrl,omitempty" db:"receipt_url"`
	Items       []TripItem  `json:"items" db:"-"`
}

type TripItem struct {
	TripID   string  `json:"tripId" db:"trip_id"`
	Name     string  `json:"name" db:"name"`
	Quantity float64 `json:"quantity" db:"quantity"`
	Unit     string  `json:"unit" db:"unit"`
}
