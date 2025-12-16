package models

type CommunityGoalStatus string

const (
	CommunityGoalStarted CommunityGoalStatus = "STARTED"
	CommunityGoalEnded   CommunityGoalStatus = "ENDED"
)

type CommunityGoal struct {
	GoalID                        string
	Title                         string
	Description                   string
	Status                        CommunityGoalStatus
	PointsContributed             int
	GoalAmount                    int
	PerStreamUserMaxContribution  int
	IsInStock                     bool
}

func (g *CommunityGoal) AmountLeft() int {
	return g.GoalAmount - g.PointsContributed
}

func CommunityGoalFromGQL(data map[string]interface{}) *CommunityGoal {
	goal := &CommunityGoal{}

	if id, ok := data["id"].(string); ok {
		goal.GoalID = id
	}
	if title, ok := data["title"].(string); ok {
		goal.Title = title
	}
	if desc, ok := data["description"].(string); ok {
		goal.Description = desc
	}
	if status, ok := data["status"].(string); ok {
		goal.Status = CommunityGoalStatus(status)
	}
	if pts, ok := data["pointsContributed"].(float64); ok {
		goal.PointsContributed = int(pts)
	}
	if amt, ok := data["goalAmount"].(float64); ok {
		goal.GoalAmount = int(amt)
	}
	if max, ok := data["perStreamUserMaximumContribution"].(float64); ok {
		goal.PerStreamUserMaxContribution = int(max)
	}
	if stock, ok := data["isInStock"].(bool); ok {
		goal.IsInStock = stock
	}

	return goal
}

func CommunityGoalFromPubSub(data map[string]interface{}) *CommunityGoal {
	goal := &CommunityGoal{}

	if id, ok := data["id"].(string); ok {
		goal.GoalID = id
	}
	if title, ok := data["title"].(string); ok {
		goal.Title = title
	}
	if desc, ok := data["description"].(string); ok {
		goal.Description = desc
	}
	if status, ok := data["status"].(string); ok {
		goal.Status = CommunityGoalStatus(status)
	}
	if pts, ok := data["points_contributed"].(float64); ok {
		goal.PointsContributed = int(pts)
	}
	if amt, ok := data["goal_amount"].(float64); ok {
		goal.GoalAmount = int(amt)
	}
	if max, ok := data["per_stream_user_maximum_contribution"].(float64); ok {
		goal.PerStreamUserMaxContribution = int(max)
	}
	if stock, ok := data["is_in_stock"].(bool); ok {
		goal.IsInStock = stock
	}

	return goal
}
