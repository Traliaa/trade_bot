package service

type OpenPositionsResponse struct {
	Code string `json:"code"`
	Data []struct {
		Adl            string `json:"adl"`
		AvailPos       string `json:"availPos"`
		AvgPx          string `json:"avgPx"`
		BaseBal        string `json:"baseBal"`
		BaseBorrowed   string `json:"baseBorrowed"`
		BaseInterest   string `json:"baseInterest"`
		BePx           string `json:"bePx"`
		BizRefId       string `json:"bizRefId"`
		BizRefType     string `json:"bizRefType"`
		CTime          string `json:"cTime"`
		Ccy            string `json:"ccy"`
		ClSpotInUseAmt string `json:"clSpotInUseAmt"`
		CloseOrderAlgo []struct {
			AlgoId          string `json:"algoId"`
			CloseFraction   string `json:"closeFraction"`
			OrdType         string `json:"ordType"`
			SlTriggerPx     string `json:"slTriggerPx"`
			SlTriggerPxType string `json:"slTriggerPxType"`
			TpTriggerPx     string `json:"tpTriggerPx"`
			TpTriggerPxType string `json:"tpTriggerPxType"`
		} `json:"closeOrderAlgo"`
		DeltaBS                string `json:"deltaBS"`
		DeltaPA                string `json:"deltaPA"`
		Fee                    string `json:"fee"`
		FundingFee             string `json:"fundingFee"`
		GammaBS                string `json:"gammaBS"`
		GammaPA                string `json:"gammaPA"`
		HedgedPos              string `json:"hedgedPos"`
		IdxPx                  string `json:"idxPx"`
		Imr                    string `json:"imr"`
		InstId                 string `json:"instId"`
		InstType               string `json:"instType"`
		Interest               string `json:"interest"`
		Last                   string `json:"last"`
		Lever                  string `json:"lever"`
		Liab                   string `json:"liab"`
		LiabCcy                string `json:"liabCcy"`
		LiqPenalty             string `json:"liqPenalty"`
		LiqPx                  string `json:"liqPx"`
		Margin                 string `json:"margin"`
		MarkPx                 string `json:"markPx"`
		MaxSpotInUseAmt        string `json:"maxSpotInUseAmt"`
		MgnMode                string `json:"mgnMode"`
		MgnRatio               string `json:"mgnRatio"`
		Mmr                    string `json:"mmr"`
		NonSettleAvgPx         string `json:"nonSettleAvgPx"`
		NotionalUsd            string `json:"notionalUsd"`
		OptVal                 string `json:"optVal"`
		PendingCloseOrdLiabVal string `json:"pendingCloseOrdLiabVal"`
		Pnl                    string `json:"pnl"`
		Pos                    string `json:"pos"`
		PosCcy                 string `json:"posCcy"`
		PosId                  string `json:"posId"`
		PosSide                string `json:"posSide"`
		QuoteBal               string `json:"quoteBal"`
		QuoteBorrowed          string `json:"quoteBorrowed"`
		QuoteInterest          string `json:"quoteInterest"`
		RealizedPnl            string `json:"realizedPnl"`
		SettledPnl             string `json:"settledPnl"`
		SpotInUseAmt           string `json:"spotInUseAmt"`
		SpotInUseCcy           string `json:"spotInUseCcy"`
		ThetaBS                string `json:"thetaBS"`
		ThetaPA                string `json:"thetaPA"`
		TradeId                string `json:"tradeId"`
		UTime                  string `json:"uTime"`
		Upl                    string `json:"upl"`
		UplLastPx              string `json:"uplLastPx"`
		UplRatio               string `json:"uplRatio"`
		UplRatioLastPx         string `json:"uplRatioLastPx"`
		UsdPx                  string `json:"usdPx"`
		VegaBS                 string `json:"vegaBS"`
		VegaPA                 string `json:"vegaPA"`
	} `json:"data"`
	Msg string `json:"msg"`
}

type Instrument struct {
	InstID   string `json:"instId"`
	TickSz   string `json:"tickSz"`
	LotSz    string `json:"lotSz"`
	MinSz    string `json:"minSz"`
	CtVal    string `json:"ctVal"`
	CtMult   string `json:"ctMult"`
	State    string `json:"state"`
	MaxMktSz string `json:"maxMktSz"`

	// ВАЖНО для корректной математики:
	CtType    string `json:"ctType"`    // "linear" / "inverse" (у OKX)
	SettleCcy string `json:"settleCcy"` // "USDT" или монета (BTC/ETH/...)
	CtValCcy  string `json:"ctValCcy"`  // "USDT"/"USD"/...
}
