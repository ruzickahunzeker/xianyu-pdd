package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (i *Items) UpdateSKU(ctx context.Context, cookieID, itemID, skuID string, priceCents, quantity int64, propertiesJSON, propertyImageURL string, enabled bool) error {
	res, err := i.DB.ExecContext(ctx, `UPDATE item_skus SET price_cent=?,quantity=?,properties_json=?,property_image_url=?,enabled=?,synced_at=? WHERE cookie_id=? AND item_id=? AND sku_id=?`, priceCents, quantity, propertiesJSON, propertyImageURL, boolToInt(enabled), time.Now().UTC().Unix(), cookieID, itemID, skuID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return i.refreshRemoteDetailTotals(ctx, cookieID, itemID)
}

func (i *Items) DeleteSKU(ctx context.Context, cookieID, itemID, skuID string) error {
	res, err := i.DB.ExecContext(ctx, `DELETE FROM item_skus WHERE cookie_id=? AND item_id=? AND sku_id=?`, cookieID, itemID, skuID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return i.refreshRemoteDetailTotals(ctx, cookieID, itemID)
}

func (i *Items) refreshRemoteDetailTotals(ctx context.Context, cookieID, itemID string) error {
	_, err := i.DB.ExecContext(ctx, `UPDATE item_remote_details SET min_price_cent=COALESCE((SELECT MIN(price_cent) FROM item_skus WHERE cookie_id=? AND item_id=?),0),max_price_cent=COALESCE((SELECT MAX(price_cent) FROM item_skus WHERE cookie_id=? AND item_id=?),0),total_quantity=COALESCE((SELECT SUM(quantity) FROM item_skus WHERE cookie_id=? AND item_id=?),0),synced_at=? WHERE cookie_id=? AND item_id=?`, cookieID, itemID, cookieID, itemID, cookieID, itemID, time.Now().UTC().Unix(), cookieID, itemID)
	return err
}

type ItemRemoteDetail struct {
	CookieID, ItemID, Description, ImagesJSON, CategoryJSON string
	MinPriceCents, MaxPriceCents, TotalQuantity             int64
	ItemStatus                                              int
	ItemStatusText, TransportFee, RawJSON                   string
	SyncedAt                                                int64
}

type ItemSKU struct {
	ID                                                      int64
	CookieID, ItemID, SKUID, InventoryID                    string
	PriceCents, Quantity                                    int64
	PropertiesJSON, PropertyImageURL, FeaturesJSON, RawJSON string
	Enabled                                                 bool
	Status, SortOrder                                       int
	SyncedAt                                                int64
}

type ItemWithSKUs struct {
	Detail *ItemRemoteDetail
	SKUs   []ItemSKU
}

func (i *Items) ReplaceRemoteDetail(ctx context.Context, detail ItemRemoteDetail, skus []ItemSKU) error {
	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	upsert := dialectUpsert(i.Dialect, []string{"cookie_id", "item_id"}, map[string]string{
		"description": "EXCLUDED.description", "images_json": "EXCLUDED.images_json", "category_json": "EXCLUDED.category_json",
		"min_price_cent": "EXCLUDED.min_price_cent", "max_price_cent": "EXCLUDED.max_price_cent", "total_quantity": "EXCLUDED.total_quantity",
		"item_status": "EXCLUDED.item_status", "item_status_text": "EXCLUDED.item_status_text", "transport_fee": "EXCLUDED.transport_fee",
		"raw_json": "EXCLUDED.raw_json", "synced_at": "EXCLUDED.synced_at",
	})
	if _, err = tx.ExecContext(ctx, `INSERT INTO item_remote_details(cookie_id,item_id,description,images_json,category_json,min_price_cent,max_price_cent,total_quantity,item_status,item_status_text,transport_fee,raw_json,synced_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`+upsert,
		detail.CookieID, detail.ItemID, detail.Description, detail.ImagesJSON, detail.CategoryJSON, detail.MinPriceCents, detail.MaxPriceCents, detail.TotalQuantity, detail.ItemStatus, detail.ItemStatusText, detail.TransportFee, detail.RawJSON, detail.SyncedAt); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM item_skus WHERE cookie_id=? AND item_id=?`, detail.CookieID, detail.ItemID); err != nil {
		return err
	}
	for _, sku := range skus {
		if _, err = tx.ExecContext(ctx, `INSERT INTO item_skus(cookie_id,item_id,sku_id,inventory_id,price_cent,quantity,properties_json,property_image_url,features_json,enabled,status,sort_order,raw_json,synced_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			detail.CookieID, detail.ItemID, sku.SKUID, sku.InventoryID, sku.PriceCents, sku.Quantity, sku.PropertiesJSON, sku.PropertyImageURL, sku.FeaturesJSON, boolToInt(sku.Enabled), sku.Status, sku.SortOrder, sku.RawJSON, detail.SyncedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (i *Items) RemoteDetailWithSKUs(ctx context.Context, cookieID, itemID string) (*ItemWithSKUs, error) {
	var d ItemRemoteDetail
	err := i.DB.QueryRowContext(ctx, `SELECT cookie_id,item_id,description,images_json,category_json,min_price_cent,max_price_cent,total_quantity,item_status,item_status_text,transport_fee,raw_json,synced_at FROM item_remote_details WHERE cookie_id=? AND item_id=?`, cookieID, itemID).Scan(
		&d.CookieID, &d.ItemID, &d.Description, &d.ImagesJSON, &d.CategoryJSON, &d.MinPriceCents, &d.MaxPriceCents, &d.TotalQuantity, &d.ItemStatus, &d.ItemStatusText, &d.TransportFee, &d.RawJSON, &d.SyncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := i.DB.QueryContext(ctx, `SELECT id,cookie_id,item_id,sku_id,inventory_id,price_cent,quantity,properties_json,property_image_url,features_json,enabled,status,sort_order,raw_json,synced_at FROM item_skus WHERE cookie_id=? AND item_id=? ORDER BY sort_order,id`, cookieID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &ItemWithSKUs{Detail: &d, SKUs: []ItemSKU{}}
	for rows.Next() {
		var s ItemSKU
		var enabled int
		if err := rows.Scan(&s.ID, &s.CookieID, &s.ItemID, &s.SKUID, &s.InventoryID, &s.PriceCents, &s.Quantity, &s.PropertiesJSON, &s.PropertyImageURL, &s.FeaturesJSON, &enabled, &s.Status, &s.SortOrder, &s.RawJSON, &s.SyncedAt); err != nil {
			return nil, err
		}
		s.Enabled = enabled != 0
		out.SKUs = append(out.SKUs, s)
	}
	return out, rows.Err()
}
