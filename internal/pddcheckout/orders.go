package pddcheckout

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Order struct {
	OrderID         string `json:"order_id"`
	GroupOrderID    string `json:"group_order_id"`
	AddressID       string `json:"address_id"`
	GoodsID         string `json:"goods_id"`
	SKUID           string `json:"sku_id"`
	GoodsName       string `json:"goods_name"`
	Spec            string `json:"spec"`
	Quantity        int64  `json:"quantity"`
	AmountCent      int64  `json:"amount_cent"`
	OrderTime       int64  `json:"order_time"`
	PaymentDeadline int64  `json:"payment_deadline"`
	PayStatus       int    `json:"pay_status"`
	ShippingStatus  int    `json:"shipping_status"`
}

type rawOrder struct {
	OrderSN             string `json:"order_sn"`
	OrderSNCamel        string `json:"orderSn"`
	GroupOrderID        string `json:"group_order_id"`
	GroupOrderIDCamel   string `json:"groupOrderId"`
	AddressID           any    `json:"address_id"`
	AddressIDCamel      any    `json:"addressId"`
	OrderAmount         any    `json:"order_amount"`
	OrderAmountCamel    any    `json:"orderAmount"`
	OrderTime           int64  `json:"order_time"`
	OrderTimeCamel      int64  `json:"orderTime"`
	NextPayTimeOut      int64  `json:"next_pay_time_out"`
	NextPayTimeOutCamel int64  `json:"nextPayTimeOut"`
	PayStatus           int    `json:"pay_status"`
	PayStatusCamel      int    `json:"payStatus"`
	ShippingStatus      int    `json:"shipping_status"`
	ShippingStatusCamel int    `json:"shippingStatus"`
	OrderGoods          []struct {
		GoodsID          any    `json:"goods_id"`
		GoodsIDCamel     any    `json:"goodsId"`
		SKUID            any    `json:"sku_id"`
		SKUIDCamel       any    `json:"skuId"`
		GoodsName        string `json:"goods_name"`
		GoodsNameCamel   string `json:"goodsName"`
		GoodsNumber      int64  `json:"goods_number"`
		GoodsNumberCamel int64  `json:"goodsNumber"`
		Spec             string `json:"spec"`
	} `json:"order_goods"`
	OrderGoodsCamel []struct {
		GoodsID     any    `json:"goodsId"`
		SKUID       any    `json:"skuId"`
		GoodsName   string `json:"goodsName"`
		GoodsNumber int64  `json:"goodsNumber"`
		Spec        string `json:"spec"`
	} `json:"orderGoods"`
}

// ParseUnpaidHTML reads the server-rendered snake_case order snapshot. It does
// not depend on generated CSS classes or visual card ordering.
func ParseUnpaidHTML(html []byte) ([]Order, error) {
	return ParseOrdersHTML(html, "1")
}

// ParseOrdersHTML parses one list from PDD's server-rendered order snapshot.
// listType "1" is unpaid and "0" is the all-orders list.
func ParseOrdersHTML(html []byte, listType string) ([]Order, error) {
	marker := `"initOdersList":`
	start := strings.Index(string(html), marker)
	if start < 0 {
		marker = `"initOrdersList":`
		start = strings.Index(string(html), marker)
	}
	if start < 0 {
		return nil, errors.New("待付款页面缺少 initOdersList")
	}
	value, err := jsonValueAt(html, start+len(marker))
	if err != nil {
		return nil, err
	}
	var lists map[string][]rawOrder
	if err := json.Unmarshal(value, &lists); err != nil {
		return nil, fmt.Errorf("解析待付款订单数据失败: %w", err)
	}
	raw := lists[listType]
	result := make([]Order, 0, len(raw))
	for _, row := range raw {
		orderSN := firstString(row.OrderSN, row.OrderSNCamel)
		if len(row.OrderGoods) == 0 && len(row.OrderGoodsCamel) > 0 {
			for _, item := range row.OrderGoodsCamel {
				row.OrderGoods = append(row.OrderGoods, struct {
					GoodsID          any    `json:"goods_id"`
					GoodsIDCamel     any    `json:"goodsId"`
					SKUID            any    `json:"sku_id"`
					SKUIDCamel       any    `json:"skuId"`
					GoodsName        string `json:"goods_name"`
					GoodsNameCamel   string `json:"goodsName"`
					GoodsNumber      int64  `json:"goods_number"`
					GoodsNumberCamel int64  `json:"goodsNumber"`
					Spec             string `json:"spec"`
				}{GoodsIDCamel: item.GoodsID, SKUIDCamel: item.SKUID, GoodsNameCamel: item.GoodsName, GoodsNumberCamel: item.GoodsNumber, Spec: item.Spec})
			}
		}
		if len(row.OrderGoods) == 0 || strings.TrimSpace(orderSN) == "" {
			continue
		}
		goods := row.OrderGoods[0]
		quantity := goods.GoodsNumber
		if quantity == 0 {
			quantity = goods.GoodsNumberCamel
		}
		orderTime := row.OrderTime
		if orderTime == 0 {
			orderTime = row.OrderTimeCamel
		}
		deadline := row.NextPayTimeOut
		if deadline == 0 {
			deadline = row.NextPayTimeOutCamel
		}
		payStatus := row.PayStatus
		if payStatus == 0 {
			payStatus = row.PayStatusCamel
		}
		shippingStatus := row.ShippingStatus
		if shippingStatus == 0 {
			shippingStatus = row.ShippingStatusCamel
		}
		result = append(result, Order{OrderID: orderSN, GroupOrderID: firstString(row.GroupOrderID, row.GroupOrderIDCamel), AddressID: textValue(firstValue(row.AddressID, row.AddressIDCamel)), GoodsID: textValue(firstValue(goods.GoodsID, goods.GoodsIDCamel)), SKUID: textValue(firstValue(goods.SKUID, goods.SKUIDCamel)), GoodsName: firstString(goods.GoodsName, goods.GoodsNameCamel), Spec: goods.Spec, Quantity: quantity, AmountCent: intValue(firstValue(row.OrderAmount, row.OrderAmountCamel)), OrderTime: orderTime, PaymentDeadline: deadline, PayStatus: payStatus, ShippingStatus: shippingStatus})
	}
	return result, nil
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
func firstValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func jsonValueAt(raw []byte, pos int) ([]byte, error) {
	for pos < len(raw) && (raw[pos] == ' ' || raw[pos] == '\n' || raw[pos] == '\r' || raw[pos] == '\t') {
		pos++
	}
	if pos >= len(raw) || (raw[pos] != '{' && raw[pos] != '[') {
		return nil, errors.New("待付款订单 JSON 起始位置无效")
	}
	open, close := raw[pos], byte('}')
	if open == '[' {
		close = ']'
	}
	depth, quoted, escaped := 0, false, false
	for i := pos; i < len(raw); i++ {
		c := raw[i]
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			continue
		}
		if c == open {
			depth++
		}
		if c == close {
			depth--
			if depth == 0 {
				return raw[pos : i+1], nil
			}
		}
	}
	return nil, errors.New("待付款订单 JSON 未闭合")
}

func textValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatInt(int64(value), 10)
	default:
		return fmt.Sprint(value)
	}
}
func intValue(v any) int64 {
	switch value := v.(type) {
	case float64:
		return int64(value)
	case string:
		if strings.Contains(value, ".") {
			f, _ := strconv.ParseFloat(value, 64)
			return int64(f*100 + 0.5)
		}
		n, _ := strconv.ParseInt(value, 10, 64)
		return n
	default:
		return 0
	}
}

func NormalizeAddress(value string) string {
	replacer := strings.NewReplacer(" ", "", "\t", "", "\n", "", "，", ",", "。", "", "；", ";")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(value)))
}
