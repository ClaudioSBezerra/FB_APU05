package handlers

import (
	"testing"

	"fb_apu05/internal/sapodata"
)

func TestConversaoKSBdaAPI(t *testing.T) {
	k1 := realizadoParaKSB1([]sapodata.RealizadoLinha{
		{PurchaseOrder: "4521982314", CostCenter: "10026440", CostElement: "4210400004", SupplierCnpj: "14.682.366/0001-00", AmountControllingCurrency: 450.004, PostingDate: "2026-02-10"},
		{PurchaseOrder: "", AmountControllingCurrency: 2.7, PostingDate: "2026-01-28"}, // sem pedido: fora
		{PurchaseOrder: "4500000001", AmountControllingCurrency: -41.43, PostingDate: "2026-03-01", IsReversalDocument: true, ReversalReferenceDocument: "2327562366"},
	})
	if len(k1) != 2 || k1[0].CNPJ != "14682366000100" || k1[0].Valor != 450 || k1[1].Valor != -41.43 || k1[1].RefEstorno != "2327562366" {
		t.Fatalf("KSB1: %+v", k1)
	}
	k2 := compromissadoParaKSB2([]sapodata.CompromissadoLinha{
		{ReferenceDocument: "10045860", ReferenceItem: "10", CostCenter: "10026070", CostElement: "4220300006", AmountControllingCurrency: 1026.63, PlannedAmountCtrlCurrency: 2000, MaterialText: "LOCACAO"},
		{ReferenceDocument: "4516963979", AmountControllingCurrency: 0, PlannedAmountCtrlCurrency: 182.67}, // consumido: fora
		{ReferenceDocument: "4516963980", AmountControllingCurrency: 10, DeletionIndicator: "L"},           // eliminado: fora
	})
	if len(k2) != 1 || k2[0].Valor != 1026.63 || k2[0].Texto != "LOCACAO" {
		t.Fatalf("KSB2: %+v", k2)
	}
}
