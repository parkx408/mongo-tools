package mongotape

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	mgo "github.com/10gen/llmgo"
	"github.com/10gen/llmgo/bson"
)

// CommandReplyOp is a struct for parsing OP_COMMANDREPLY as defined here: https://github.com/mongodb/mongo/blob/master/src/mongo/rpc/command_reply.h.
// Although this file parses the wire protocol message into a more useable struct, it does not currently provide functionality to execute
// the operation, as it is not implemented fully in llmgo.
type CommandReplyOp struct {
	Header MsgHeader
	mgo.CommandReplyOp
	Docs         []bson.Raw
	Latency      time.Duration
	cursorCached bool
	cursorId     int64
}

func (op *CommandReplyOp) OpCode() OpCode {
	return OpCodeCommandReply
}

// Meta returns metadata about the operation, useful for analysis of traffic.
// Currently only returns 'unknown' as it is not fully parsed and analyzed.
func (op *CommandReplyOp) Meta() OpMetadata {
	return OpMetadata{"op_commandreply",
		"",
		"",
		map[string]interface{}{
			"metadata":      op.Metadata,
			"command_reply": op.CommandReply,
			"output_docs":   op.OutputDocs,
		},
	}
}

func (op *CommandReplyOp) String() string {
	commandReplyString, metadataString, outputDocsString, err := op.getOpBodyString()
	if err != nil {
		return fmt.Sprintf("%v", err)
	}
	return fmt.Sprintf("CommandReply %v %v %v", commandReplyString, metadataString, outputDocsString)
}

func (op *CommandReplyOp) Abbreviated(chars int) string {
	commandReplyString, metadataString, outputDocsString, err := op.getOpBodyString()
	if err != nil {
		return fmt.Sprintf("%v", err)
	}
	return fmt.Sprintf("CommandReply %v %v", Abbreviate(commandReplyString, chars),
		Abbreviate(metadataString, chars), Abbreviate(outputDocsString, chars))
}

// getCursorId implements the Replyable interface method of the same name.
// It returns the cursorId associated with this CommandReplyOp. It returns an
// error if there is an issue unmarshalling the underlying bson. getCursorId also
// caches in the CommandReplyOp struct so that multiple calls to this function do
// not incur the cost of unmarshalling the bson.
func (op *CommandReplyOp) getCursorId() (int64, error) {
	if op.cursorCached {
		return op.cursorId, nil
	}
	doc := &struct {
		Cursor struct {
			Id int64 `bson:"id"`
		} `bson: "cursor"`
	}{}
	replyArgs := op.CommandReply.(*bson.Raw)
	err := replyArgs.Unmarshal(doc)
	if err != nil {
		// can happen if there's corrupt bson in the doc.
		return 0, fmt.Errorf("failed to unmarshal bson.Raw into struct: %v", err)
	}
	op.cursorCached = true
	op.cursorId = doc.Cursor.Id
	return op.cursorId, nil
}

func (op *CommandReplyOp) getOpBodyString() (string, string, string, error) {
	commandReplyDoc, err := ConvertBSONValueToJSON(op.CommandReply)
	if err != nil {
		return "", "", "", fmt.Errorf("ConvertBSONValueToJSON err: %#v - %v", op, err)
	}

	commandReplyAsJson, err := json.Marshal(commandReplyDoc)
	if err != nil {
		return "", "", "", fmt.Errorf("json marshal err: %#v - %v", op, err)
	}

	metadataDocs, err := ConvertBSONValueToJSON(op.Metadata)
	if err != nil {
		return "", "", "", fmt.Errorf("ConvertBSONValueToJSON err: %#v - %v", op, err)
	}

	metadataAsJson, err := json.Marshal(metadataDocs)
	if err != nil {
		return "", "", "", fmt.Errorf("json marshal err: %#v - %v", op, err)
	}

	var outputDocsString string

	if len(op.OutputDocs) != 0 {
		outputDocs, err := ConvertBSONValueToJSON(op.OutputDocs)
		if err != nil {
			return "", "", "", fmt.Errorf("ConvertBSONValueToJSON err: %#v - %v", op, err)
		}

		outputDocsAsJson, err := json.Marshal(outputDocs)
		if err != nil {
			return "", "", "", fmt.Errorf("json marshal err: %#v - %v", op, err)
		}
		outputDocsString = string(outputDocsAsJson)
	}
	return string(commandReplyAsJson), string(metadataAsJson), outputDocsString, nil
}

func (op *CommandReplyOp) FromReader(r io.Reader) error {
	commandReplyAsSlice, err := ReadDocument(r)
	if err != nil {
		return err
	}
	op.CommandReply = &bson.Raw{}
	err = bson.Unmarshal(commandReplyAsSlice, op.CommandReply)
	if err != nil {
		return err
	}

	metadataAsSlice, err := ReadDocument(r)
	if err != nil {
		return err
	}
	op.Metadata = &bson.Raw{}
	err = bson.Unmarshal(metadataAsSlice, op.Metadata)
	if err != nil {
		return err
	}

	lengthRead := len(commandReplyAsSlice) + len(metadataAsSlice)
	op.OutputDocs = make([]interface{}, 0)
	docLen := 0
	for lengthRead+docLen < int(op.Header.MessageLength)-MsgHeaderLen {
		docAsSlice, err := ReadDocument(r)
		doc := &bson.Raw{}
		err = bson.Unmarshal(docAsSlice, doc)
		if err != nil {
			return err
		}
		docLen += len(docAsSlice)
		op.OutputDocs = append(op.OutputDocs, doc)
	}
	return nil
}

// Execute logs a warning and returns nil because OP_COMMANDREPLY cannot yet be handled fully by mongotape.

func (op *CommandReplyOp) Execute(session *mgo.Session) (Replyable, error) {
	userInfoLogger.Log(Always, "Skipping unimplemented op: OP_COMMANDREPLY")
	return nil, nil
}

func (op *CommandReplyOp) getNumReturned() int {
	return len(op.Docs)
}

func (op *CommandReplyOp) getLatencyMicros() int64 {
	return int64(op.Latency / (time.Microsecond))
}
func (op *CommandReplyOp) getErrors() []error {
	if len(op.Docs) == 0 {
		return nil
	}

	firstDoc := bson.D{}
	err := op.Docs[0].Unmarshal(&firstDoc)
	if err != nil {
		panic("failed to unmarshal Raw into bson.D")
	}
	return extractErrorsFromDoc(&firstDoc)
}
