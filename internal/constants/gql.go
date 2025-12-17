package constants

type GQLOperation struct {
	OperationName string                 `json:"operationName"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	Extensions    GQLExtensions          `json:"extensions"`
}

type GQLExtensions struct {
	PersistedQuery GQLPersistedQuery `json:"persistedQuery"`
}

type GQLPersistedQuery struct {
	Version    int    `json:"version"`
	SHA256Hash string `json:"sha256Hash"`
}

func NewGQLOperation(name, hash string) GQLOperation {
	return GQLOperation{
		OperationName: name,
		Extensions: GQLExtensions{
			PersistedQuery: GQLPersistedQuery{
				Version:    1,
				SHA256Hash: hash,
			},
		},
	}
}

func (g GQLOperation) WithVariables(vars map[string]interface{}) GQLOperation {
	g.Variables = vars
	return g
}

var (
	WithIsStreamLiveQuery = NewGQLOperation(
		"WithIsStreamLiveQuery",
		"04e46329a6786ff3a81c01c50bfa5d725902507a0deb83b0edbf7abe7a3716ea",
	)

	PlaybackAccessToken = NewGQLOperation(
		"PlaybackAccessToken",
		"ed230aa1e33e07eebb8928504583da78a5173989fadfb1ac94be06a04f3cdbe9",
	)

	VideoPlayerStreamInfoOverlayChannel = NewGQLOperation(
		"VideoPlayerStreamInfoOverlayChannel",
		"198492e0857f6aedead9665c81c5a06d67b25b58034649687124083ff288597d",
	)

	ClaimCommunityPoints = NewGQLOperation(
		"ClaimCommunityPoints",
		"46aaeebe02c99afdf4fc97c7c0cba964124bf6b0af229395f1f6d1feed05b3d0",
	)

	CommunityMomentCalloutClaim = NewGQLOperation(
		"CommunityMomentCallout_Claim",
		"e2d67415aead910f7f9ceb45a77b750a1e1d9622c936d832328a0689e054db62",
	)

	DropsPageClaimDropRewards = NewGQLOperation(
		"DropsPage_ClaimDropRewards",
		"a455deea71bdc9015b78eb49f4acfbce8baa7ccbedd28e549bb025bd0f751930",
	)

	ChannelPointsContext = NewGQLOperation(
		"ChannelPointsContext",
		"374314de591e69925fce3ddc2bcf085796f56ebb8cad67a0daa3165c03adc345",
	)

	JoinRaid = NewGQLOperation(
		"JoinRaid",
		"c6a332a86d1087fbbb1a8623aa01bd1313d2386e7c63be60fdb2d1901f01a4ae",
	)

	Inventory = NewGQLOperation(
		"Inventory",
		"d86775d0ef16a63a33ad52e80eaff963b2d5b72fada7c991504a57496e1d8e4b",
	).WithVariables(map[string]interface{}{"fetchRewardCampaigns": true})

	MakePrediction = NewGQLOperation(
		"MakePrediction",
		"b44682ecc88358817009f20e69d75081b1e58825bb40aa53d5dbadcc17c881d8",
	)

	ViewerDropsDashboard = NewGQLOperation(
		"ViewerDropsDashboard",
		"5a4da2ab3d5b47c9f9ce864e727b2cb346af1e3ea8b897fe8f704a97ff017619",
	).WithVariables(map[string]interface{}{"fetchRewardCampaigns": true})

	DropCampaignDetails = NewGQLOperation(
		"DropCampaignDetails",
		"039277bf98f3130929262cc7c6efd9c141ca3749cb6dca442fc8ead9a53f77c1",
	)

	DropsHighlightServiceAvailableDrops = NewGQLOperation(
		"DropsHighlightService_AvailableDrops",
		"9a62a09bce5b53e26e64a671e530bc599cb6aab1e5ba3cbd5d85966d3940716f",
	)

	GetIDFromLogin = NewGQLOperation(
		"GetIDFromLogin",
		"94e82a7b1e3c21e186daa73ee2afc4b8f23bade1fbbff6fe8ac133f50a2f58ca",
	)

	ChannelFollows = NewGQLOperation(
		"ChannelFollows",
		"eecf815273d3d949e5cf0085cc5084cd8a1b5b7b6f7990cf43cb0beadf546907",
	).WithVariables(map[string]interface{}{"limit": 100, "order": "ASC"})

	UserPointsContribution = NewGQLOperation(
		"UserPointsContribution",
		"23ff2c2d60708379131178742327ead913b93b1bd6f665517a6d9085b73f661f",
	)

	ContributeCommunityPointsCommunityGoal = NewGQLOperation(
		"ContributeCommunityPointsCommunityGoal",
		"5774f0ea5d89587d73021a2e03c3c44777d903840c608754a1be519f51e37bb6",
	)
)
