package azure

type refListResponse struct {
	Value []refListResponseItem `json:"value"`
	Count int                   `json:"count"`
}

type refListResponseItem struct {
	Name     string `json:"name"`
	ObjectId string `json:"objectId"`
	Url      string `json:"url"`
}
