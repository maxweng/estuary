package main

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"golang.org/x/xerrors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/filecoin-project/go-address"
	datatransfer "github.com/filecoin-project/go-data-transfer"
	"github.com/libp2p/go-libp2p-core/peer"
	drpc "github.com/whyrusleeping/estuary/drpc"
	"github.com/whyrusleeping/estuary/filclient"
	"github.com/whyrusleeping/estuary/util"
)

type Shuttle struct {
	gorm.Model

	Handle string `gorm:"unique"`
	Token  string

	LastConnection time.Time
	Host           string
	PeerID         string

	Open bool
}

type shuttleConnection struct {
	handle string

	hostname string
	addrInfo peer.AddrInfo
	address  address.Address

	cmds    chan *drpc.Command
	closing chan struct{}
}

func (dc *shuttleConnection) sendMessage(ctx context.Context, cmd *drpc.Command) error {
	select {
	case dc.cmds <- cmd:
		return nil
	case <-dc.closing:
		return ErrNoShuttleConnection
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (cm *ContentManager) registerShuttleConnection(handle string, hello *drpc.Hello) (chan *drpc.Command, func(), error) {
	cm.shuttlesLk.Lock()
	defer cm.shuttlesLk.Unlock()
	_, ok := cm.shuttles[handle]
	if ok {
		log.Warn("registering shuttle but found existing connection")
		return nil, nil, fmt.Errorf("shuttle already connected")
	}

	var hostname string
	u, err := url.Parse(hello.Host)
	if err != nil {
		log.Errorf("shuttle had invalid hostname %q: %s", hello.Host, err)
	} else {
		hostname = u.Host
	}

	d := &shuttleConnection{
		handle:   handle,
		address:  hello.Address,
		addrInfo: hello.AddrInfo,
		hostname: hostname,
		cmds:     make(chan *drpc.Command, 32),
		closing:  make(chan struct{}),
	}

	cm.shuttles[handle] = d

	return d.cmds, func() {
		close(d.closing)
		cm.shuttlesLk.Lock()
		outd, ok := cm.shuttles[handle]
		if ok {
			if outd == d {
				delete(cm.shuttles, handle)
			}
		}
		cm.shuttlesLk.Unlock()

	}, nil
}

var ErrNilParams = fmt.Errorf("shuttle message had nil params")

func (cm *ContentManager) processShuttleMessage(handle string, msg *drpc.Message) error {
	ctx, span := cm.tracer.Start(context.TODO(), "processShuttleMessage")
	defer span.End()

	switch msg.Op {
	case drpc.OP_UpdatePinStatus:
		ups := msg.Params.UpdatePinStatus
		if ups == nil {
			return ErrNilParams
		}
		cm.UpdatePinStatus(handle, ups.DBID, ups.Status)
		return nil
	case drpc.OP_PinComplete:
		param := msg.Params.PinComplete
		if param == nil {
			return ErrNilParams
		}

		if err := cm.handlePinningComplete(ctx, handle, param); err != nil {
			log.Errorw("handling pin complete message failed", "shuttle", handle, "err", err)
		}
		return nil
	case drpc.OP_CommPComplete:
		param := msg.Params.CommPComplete
		if param == nil {
			return ErrNilParams
		}

		if err := cm.handleRpcCommPComplete(ctx, handle, param); err != nil {
			log.Errorf("handling commp complete message from shuttle %s: %s", handle, err)
		}
		return nil
	case drpc.OP_TransferStarted:
		param := msg.Params.TransferStarted
		if param == nil {
			return ErrNilParams
		}

		if err := cm.handleRpcTransferStarted(ctx, handle, param); err != nil {
			log.Errorf("handling transfer started message from shuttle %s: %s", handle, err)
		}
		return nil
	case drpc.OP_TransferStatus:
		param := msg.Params.TransferStatus
		if param == nil {
			return ErrNilParams
		}

		if err := cm.handleRpcTransferStatus(ctx, handle, param); err != nil {
			log.Errorf("handling transfer status message from shuttle %s: %s", handle, err)
		}
		return nil
	default:
		return fmt.Errorf("unrecognized message op: %q", msg.Op)
	}
}

var ErrNoShuttleConnection = fmt.Errorf("no connection to requested shuttle")

func (cm *ContentManager) sendShuttleCommand(ctx context.Context, handle string, cmd *drpc.Command) error {
	cm.shuttlesLk.Lock()
	d, ok := cm.shuttles[handle]
	cm.shuttlesLk.Unlock()
	if ok {
		return d.sendMessage(ctx, cmd)
	}

	return ErrNoShuttleConnection
}

func (cm *ContentManager) shuttleIsOnline(handle string) bool {
	cm.shuttlesLk.Lock()
	d, ok := cm.shuttles[handle]
	cm.shuttlesLk.Unlock()
	if !ok {
		return false
	}

	select {
	case <-d.closing:
		return false
	default:
		return true
	}
}

func (cm *ContentManager) shuttleAddrInfo(handle string) *peer.AddrInfo {
	cm.shuttlesLk.Lock()
	defer cm.shuttlesLk.Unlock()
	d, ok := cm.shuttles[handle]
	if ok {
		return &d.addrInfo
	}
	return nil
}

func (cm *ContentManager) shuttleHostName(handle string) string {
	cm.shuttlesLk.Lock()
	defer cm.shuttlesLk.Unlock()
	d, ok := cm.shuttles[handle]
	if ok {
		return d.hostname
	}
	return ""
}

func (cm *ContentManager) handleRpcCommPComplete(ctx context.Context, handle string, resp *drpc.CommPComplete) error {
	ctx, span := cm.tracer.Start(ctx, "handleRpcCommPComplete")
	defer span.End()

	opcr := PieceCommRecord{
		Data:  util.DbCID{resp.Data},
		Piece: util.DbCID{resp.CommP},
		Size:  resp.Size,
	}

	if err := cm.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&opcr).Error; err != nil {
		return err
	}

	return nil
}

func (cm *ContentManager) handleRpcTransferStarted(ctx context.Context, handle string, param *drpc.TransferStarted) error {
	if err := cm.DB.Model(contentDeal{}).Where("id = ?", param.DealDBID).UpdateColumns(map[string]interface{}{
		"dt_chan":           param.Chanid,
		"transfer_started":  time.Now(),
		"transfer_finished": time.Time{},
	}).Error; err != nil {
		return xerrors.Errorf("failed to update deal with channel ID: %w", err)
	}

	log.Infow("Started data transfer on shuttle", "chanid", param.Chanid, "shuttle", handle)
	return nil
}

func (cm *ContentManager) handleRpcTransferStatus(ctx context.Context, handle string, param *drpc.TransferStatus) error {
	log.Infof("handling transfer status rpc update: %d %v", param.DealDBID, param.State == nil)
	if param.Failed {
		var cd contentDeal
		if err := cm.DB.First(&cd, "id = ?", param.DealDBID).Error; err != nil {
			return err
		}

		miner, err := cd.MinerAddr()
		if err != nil {
			return err
		}

		if oerr := cm.recordDealFailure(&DealFailureError{
			Miner:   miner,
			Phase:   "start-data-transfer-remote",
			Message: fmt.Sprintf("failure from shuttle %s: %s", handle, param.Message),
			Content: cd.Content,
		}); oerr != nil {
			return oerr
		}

		cm.updateTransferStatus(ctx, handle, param.DealDBID, &filclient.ChannelState{
			Status:  datatransfer.Failed,
			Message: fmt.Sprintf("failure from shuttle %s: %s", handle, param.Message),
		})
		return nil
	}
	cm.updateTransferStatus(ctx, handle, param.DealDBID, param.State)
	return nil
}