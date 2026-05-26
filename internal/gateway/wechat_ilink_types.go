package gateway

// ============================================================================
// iLink Bot API Types
//
// These types correspond to the Tencent iLink Bot REST API used for
// WeChat bot integration. The API uses JSON for request/response bodies.
// ============================================================================

// ILinkBaseInfo is attached to every outgoing CGI request.
type ILinkBaseInfo struct {
	ChannelVersion string `json:"channel_version,omitempty"`
}

// ILinkAPIStatus is the base response status embedded in all API responses.
type ILinkAPIStatus struct {
	Ret     int    `json:"ret,omitempty"`
	Errcode int    `json:"errcode,omitempty"`
	Errmsg  string `json:"errmsg,omitempty"`
}

// Message types (message_type field).
const (
	ILinkMsgTypeNone = 0
	ILinkMsgTypeUser = 1
	ILinkMsgTypeBot  = 2
)

// Message item types (type field in ILinkMessageItem).
const (
	ILinkItemTypeNone  = 0
	ILinkItemTypeText  = 1
	ILinkItemTypeImage = 2
	ILinkItemTypeVoice = 3
	ILinkItemTypeFile  = 4
	ILinkItemTypeVideo = 5
)

// Message states.
const (
	ILinkMsgStateNone       = 0
	ILinkMsgStateGenerating = 1
	ILinkMsgStateFinish     = 2
)

// Typing status values.
const (
	ILinkTypingTyping = 1
	ILinkTypingCancel = 2
)

// Upload media type constants.
const (
	ILinkUploadMediaImage = 1
	ILinkUploadMediaVideo = 2
	ILinkUploadMediaFile  = 3
	ILinkUploadMediaVoice = 4
)

// ============================================================================
// Message Types
// ============================================================================

// ILinkTextItem represents a text message item.
type ILinkTextItem struct {
	Text string `json:"text,omitempty"`
}

// ILinkCDNMedia represents a CDN media reference with encryption.
type ILinkCDNMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	AesKey            string `json:"aes_key,omitempty"` // base64 encoded
	EncryptType       int    `json:"encrypt_type,omitempty"`
	FullURL           string `json:"full_url,omitempty"`
}

// ILinkImageItem represents an image message item.
type ILinkImageItem struct {
	Media       *ILinkCDNMedia `json:"media,omitempty"`
	ThumbMedia  *ILinkCDNMedia `json:"thumb_media,omitempty"`
	Aeskey      string         `json:"aeskey,omitempty"`
	Url         string         `json:"url,omitempty"`
	MidSize     int64          `json:"mid_size,omitempty"`
	ThumbSize   int64          `json:"thumb_size,omitempty"`
	ThumbHeight int            `json:"thumb_height,omitempty"`
	ThumbWidth  int            `json:"thumb_width,omitempty"`
	HDSize      int64          `json:"hd_size,omitempty"`
}

// ILinkVoiceItem represents a voice message item.
// Text field contains server-side ASR transcription when available.
type ILinkVoiceItem struct {
	Media         *ILinkCDNMedia `json:"media,omitempty"`
	EncodeType    int            `json:"encode_type,omitempty"`
	BitsPerSample int            `json:"bits_per_sample,omitempty"`
	SampleRate    int            `json:"sample_rate,omitempty"`
	Playtime      int            `json:"playtime,omitempty"`
	Text          string         `json:"text,omitempty"`
}

// ILinkFileItem represents a file message item.
type ILinkFileItem struct {
	Media    *ILinkCDNMedia `json:"media,omitempty"`
	FileName string         `json:"file_name,omitempty"`
	MD5      string         `json:"md5,omitempty"`
	Len      string         `json:"len,omitempty"`
}

// ILinkVideoItem represents a video message item.
type ILinkVideoItem struct {
	Media       *ILinkCDNMedia `json:"media,omitempty"`
	VideoSize   int64          `json:"video_size,omitempty"`
	PlayLength  int            `json:"play_length,omitempty"`
	VideoMD5    string         `json:"video_md5,omitempty"`
	ThumbMedia  *ILinkCDNMedia `json:"thumb_media,omitempty"`
	ThumbSize   int64          `json:"thumb_size,omitempty"`
	ThumbHeight int            `json:"thumb_height,omitempty"`
	ThumbWidth  int            `json:"thumb_width,omitempty"`
}

// ILinkRefMessage represents a referenced/replied-to message.
type ILinkRefMessage struct {
	MessageItem *ILinkMessageItem `json:"message_item,omitempty"`
	Title       string            `json:"title,omitempty"`
}

// ILinkMessageItem is a single item in a message's item_list.
type ILinkMessageItem struct {
	Type         int              `json:"type,omitempty"`
	CreateTimeMs int64            `json:"create_time_ms,omitempty"`
	UpdateTimeMs int64            `json:"update_time_ms,omitempty"`
	IsCompleted  bool             `json:"is_completed,omitempty"`
	MsgID        string           `json:"msg_id,omitempty"`
	RefMsg       *ILinkRefMessage `json:"ref_msg,omitempty"`
	TextItem     *ILinkTextItem   `json:"text_item,omitempty"`
	ImageItem    *ILinkImageItem  `json:"image_item,omitempty"`
	VoiceItem    *ILinkVoiceItem  `json:"voice_item,omitempty"`
	FileItem     *ILinkFileItem   `json:"file_item,omitempty"`
	VideoItem    *ILinkVideoItem  `json:"video_item,omitempty"`
}

// ILinkMessage is a WeChat message from the iLink API.
type ILinkMessage struct {
	Seq          int                `json:"seq,omitempty"`
	MessageID    int64              `json:"message_id,omitempty"`
	FromUserID   string             `json:"from_user_id,omitempty"`
	ToUserID     string             `json:"to_user_id,omitempty"`
	ClientID     string             `json:"client_id,omitempty"`
	CreateTimeMs int64              `json:"create_time_ms,omitempty"`
	UpdateTimeMs int64              `json:"update_time_ms,omitempty"`
	DeleteTimeMs int64              `json:"delete_time_ms,omitempty"`
	SessionID    string             `json:"session_id,omitempty"`
	GroupID      string             `json:"group_id,omitempty"`
	MessageType  int                `json:"message_type,omitempty"`
	MessageState int                `json:"message_state,omitempty"`
	ItemList     []ILinkMessageItem `json:"item_list,omitempty"`
	ContextToken string             `json:"context_token,omitempty"`
}

// ============================================================================
// API Request/Response Types
// ============================================================================

// ILinkGetUpdatesReq is the request for the getupdates endpoint.
type ILinkGetUpdatesReq struct {
	SyncBuf       string        `json:"sync_buf,omitempty"`
	GetUpdatesBuf string        `json:"get_updates_buf,omitempty"`
	BaseInfo      ILinkBaseInfo `json:"base_info,omitempty"`
}

// ILinkGetUpdatesResp is the response from the getupdates endpoint.
type ILinkGetUpdatesResp struct {
	ILinkAPIStatus
	Msgs                 []ILinkMessage `json:"msgs,omitempty"`
	SyncBuf              string         `json:"sync_buf,omitempty"`
	GetUpdatesBuf        string         `json:"get_updates_buf,omitempty"`
	LongpollingTimeoutMs int            `json:"longpolling_timeout_ms,omitempty"`
}

// ILinkSendMessageReq is the request for the sendmessage endpoint.
type ILinkSendMessageReq struct {
	Msg      ILinkMessage  `json:"msg,omitempty"`
	BaseInfo ILinkBaseInfo `json:"base_info,omitempty"`
}

// ILinkSendMessageResp is the response from the sendmessage endpoint.
type ILinkSendMessageResp struct {
	ILinkAPIStatus
}

// ILinkGetConfigReq is the request for the getconfig endpoint.
type ILinkGetConfigReq struct {
	IlinkUserID  string        `json:"ilink_user_id,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
	BaseInfo     ILinkBaseInfo `json:"base_info,omitempty"`
}

// ILinkGetConfigResp is the response from the getconfig endpoint.
type ILinkGetConfigResp struct {
	ILinkAPIStatus
	TypingTicket string `json:"typing_ticket,omitempty"`
}

// ILinkSendTypingReq is the request for the sendtyping endpoint.
type ILinkSendTypingReq struct {
	IlinkUserID  string        `json:"ilink_user_id,omitempty"`
	TypingTicket string        `json:"typing_ticket,omitempty"`
	Status       int           `json:"status,omitempty"` // 1=typing, 2=cancel
	BaseInfo     ILinkBaseInfo `json:"base_info,omitempty"`
}

// ILinkSendTypingResp is the response from the sendtyping endpoint.
type ILinkSendTypingResp struct {
	ILinkAPIStatus
}

// ILinkGetUploadURLReq is the request for the getuploadurl endpoint.
type ILinkGetUploadURLReq struct {
	Filekey         string        `json:"filekey,omitempty"`
	MediaType       int           `json:"media_type,omitempty"`
	ToUserID        string        `json:"to_user_id,omitempty"`
	Rawsize         int64         `json:"rawsize,omitempty"`
	RawfileMD5      string        `json:"rawfilemd5,omitempty"`
	Filesize        int64         `json:"filesize,omitempty"`
	ThumbRawsize    int64         `json:"thumb_rawsize,omitempty"`
	ThumbRawfileMD5 string        `json:"thumb_rawfilemd5,omitempty"`
	ThumbFilesize   int64         `json:"thumb_filesize,omitempty"`
	NoNeedThumb     bool          `json:"no_need_thumb,omitempty"`
	Aeskey          string        `json:"aeskey,omitempty"` // hex-encoded 16-byte AES key
	BaseInfo        ILinkBaseInfo `json:"base_info,omitempty"`
}

// ILinkGetUploadURLResp is the response from the getuploadurl endpoint.
type ILinkGetUploadURLResp struct {
	ILinkAPIStatus
	UploadParam      string `json:"upload_param,omitempty"`
	ThumbUploadParam string `json:"thumb_upload_param,omitempty"`
	UploadFullURL    string `json:"upload_full_url,omitempty"`
}

// ============================================================================
// QR Code Auth Types
// ============================================================================

// ILinkQRCodeResponse is the response from get_bot_qrcode.
type ILinkQRCodeResponse struct {
	Qrcode           string `json:"qrcode"`
	QrcodeImgContent string `json:"qrcode_img_content"`
}

// ILinkStatusResponse is the response from get_qrcode_status.
type ILinkStatusResponse struct {
	Status       string `json:"status"` // "wait", "scaned", "confirmed", "expired", "scaned_but_redirect"
	BotToken     string `json:"bot_token,omitempty"`
	IlinkBotID   string `json:"ilink_bot_id,omitempty"`
	Baseurl      string `json:"baseurl,omitempty"`
	IlinkUserID  string `json:"ilink_user_id,omitempty"`
	RedirectHost string `json:"redirect_host,omitempty"`
}
