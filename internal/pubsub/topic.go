package pubsub

import "fmt"

type TopicType string

const (
	TopicCommunityPointsUser    TopicType = "community-points-user-v1"
	TopicPredictionsUser        TopicType = "predictions-user-v1"
	TopicVideoPlaybackByID      TopicType = "video-playback-by-id"
	TopicRaid                   TopicType = "raid"
	TopicPredictionsChannel     TopicType = "predictions-channel-v1"
	TopicCommunityMomentsChannel TopicType = "community-moments-channel-v1"
	TopicCommunityPointsChannel TopicType = "community-points-channel-v1"
)

type Topic struct {
	Type      TopicType
	ChannelID string
}

func NewTopic(topicType TopicType, channelID string) Topic {
	return Topic{Type: topicType, ChannelID: channelID}
}

func (t Topic) String() string {
	return fmt.Sprintf("%s.%s", t.Type, t.ChannelID)
}

func (t Topic) IsUserTopic() bool {
	return t.Type == TopicCommunityPointsUser || t.Type == TopicPredictionsUser
}

func ParseTopic(topicStr string) (Topic, error) {
	var topic Topic
	for i := len(topicStr) - 1; i >= 0; i-- {
		if topicStr[i] == '.' {
			topic.Type = TopicType(topicStr[:i])
			topic.ChannelID = topicStr[i+1:]
			return topic, nil
		}
	}
	return topic, fmt.Errorf("invalid topic format: %s", topicStr)
}
