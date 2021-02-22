<!DOCTYPE html>

<html>
  <head>
    <title>Protocol Documentation</title>
    <meta charset="UTF-8">
    <link rel="stylesheet" type="text/css" href="https://fonts.googleapis.com/css?family=Ubuntu:400,700,400italic"/>
    <style>
      body {
        width: 60em;
        margin: 1em auto;
        color: #222;
        font-family: "Ubuntu", sans-serif;
        padding-bottom: 4em;
      }

      h1 {
        font-weight: normal;
        border-bottom: 1px solid #aaa;
        padding-bottom: 0.5ex;
      }

      h2 {
        border-bottom: 1px solid #aaa;
        padding-bottom: 0.5ex;
        margin: 1.5em 0;
      }

      h3 {
        font-weight: normal;
        border-bottom: 1px solid #aaa;
        padding-bottom: 0.5ex;
      }

      a {
        text-decoration: none;
        color: #567e25;
      }

      table {
        width: 100%;
        font-size: 80%;
        border-collapse: collapse;
      }

      thead {
        font-weight: 700;
        background-color: #dcdcdc;
      }

      tbody tr:nth-child(even) {
        background-color: #fbfbfb;
      }

      td {
        border: 1px solid #ccc;
        padding: 0.5ex 2ex;
      }

      td p {
        text-indent: 1em;
        margin: 0;
      }

      td p:nth-child(1) {
        text-indent: 0;  
      }

       
      .field-table td:nth-child(1) {  
        width: 10em;
      }
      .field-table td:nth-child(2) {  
        width: 10em;
      }
      .field-table td:nth-child(3) {  
        width: 6em;
      }
      .field-table td:nth-child(4) {  
        width: auto;
      }

       
      .extension-table td:nth-child(1) {  
        width: 10em;
      }
      .extension-table td:nth-child(2) {  
        width: 10em;
      }
      .extension-table td:nth-child(3) {  
        width: 10em;
      }
      .extension-table td:nth-child(4) {  
        width: 5em;
      }
      .extension-table td:nth-child(5) {  
        width: auto;
      }

       
      .enum-table td:nth-child(1) {  
        width: 10em;
      }
      .enum-table td:nth-child(2) {  
        width: 10em;
      }
      .enum-table td:nth-child(3) {  
        width: auto;
      }

       
      .scalar-value-types-table tr {
        height: 3em;
      }

       
      #toc-container ul {
        list-style-type: none;
        padding-left: 1em;
        line-height: 180%;
        margin: 0;
      }
      #toc > li > a {
        font-weight: bold;
      }

       
      .file-heading {
        width: 100%;
        display: table;
        border-bottom: 1px solid #aaa;
        margin: 4em 0 1.5em 0;
      }
      .file-heading h2 {
        border: none;
        display: table-cell;
      }
      .file-heading a {
        text-align: right;
        display: table-cell;
      }

       
      .badge {
        width: 1.6em;
        height: 1.6em;
        display: inline-block;

        line-height: 1.6em;
        text-align: center;
        font-weight: bold;
        font-size: 60%;

        color: #89ba48;
        background-color: #dff0c8;

        margin: 0.5ex 1em 0.5ex -1em;
        border: 1px solid #fbfbfb;
        border-radius: 1ex;
      }
    </style>

    
    <link rel="stylesheet" type="text/css" href="stylesheet.css"/>
  </head>

  <body>

    <h1 id="title">Protocol Documentation</h1>

    <h2>Table of Contents</h2>

    <div id="toc-container">
      <ul id="toc">
        
          
          <li>
            <a href="#ibcgo%2fapps%2ftransfer%2fv1%2ftransfer.proto">ibcgo/apps/transfer/v1/transfer.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.DenomTrace"><span class="badge">M</span>DenomTrace</a>
                </li>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.FungibleTokenPacketData"><span class="badge">M</span>FungibleTokenPacketData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.Params"><span class="badge">M</span>Params</a>
                </li>
              
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fapps%2ftransfer%2fv1%2fgenesis.proto">ibcgo/apps/transfer/v1/genesis.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.GenesisState"><span class="badge">M</span>GenesisState</a>
                </li>
              
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fapps%2ftransfer%2fv1%2fquery.proto">ibcgo/apps/transfer/v1/query.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.QueryDenomTraceRequest"><span class="badge">M</span>QueryDenomTraceRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.QueryDenomTraceResponse"><span class="badge">M</span>QueryDenomTraceResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.QueryDenomTracesRequest"><span class="badge">M</span>QueryDenomTracesRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.QueryDenomTracesResponse"><span class="badge">M</span>QueryDenomTracesResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.QueryParamsRequest"><span class="badge">M</span>QueryParamsRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.QueryParamsResponse"><span class="badge">M</span>QueryParamsResponse</a>
                </li>
              
              
              
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.Query"><span class="badge">S</span>Query</a>
                </li>
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fclient%2fv1%2fclient.proto">ibcgo/core/client/v1/client.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.client.v1.ClientConsensusStates"><span class="badge">M</span>ClientConsensusStates</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.ClientUpdateProposal"><span class="badge">M</span>ClientUpdateProposal</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.ConsensusStateWithHeight"><span class="badge">M</span>ConsensusStateWithHeight</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.Height"><span class="badge">M</span>Height</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.IdentifiedClientState"><span class="badge">M</span>IdentifiedClientState</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.Params"><span class="badge">M</span>Params</a>
                </li>
              
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fapps%2ftransfer%2fv1%2ftx.proto">ibcgo/apps/transfer/v1/tx.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.MsgTransfer"><span class="badge">M</span>MsgTransfer</a>
                </li>
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.MsgTransferResponse"><span class="badge">M</span>MsgTransferResponse</a>
                </li>
              
              
              
              
                <li>
                  <a href="#ibcgo.apps.transfer.v1.Msg"><span class="badge">S</span>Msg</a>
                </li>
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fchannel%2fv1%2fchannel.proto">ibcgo/core/channel/v1/channel.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.Acknowledgement"><span class="badge">M</span>Acknowledgement</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.Channel"><span class="badge">M</span>Channel</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.Counterparty"><span class="badge">M</span>Counterparty</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.IdentifiedChannel"><span class="badge">M</span>IdentifiedChannel</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.Packet"><span class="badge">M</span>Packet</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.PacketState"><span class="badge">M</span>PacketState</a>
                </li>
              
              
                <li>
                  <a href="#ibcgo.core.channel.v1.Order"><span class="badge">E</span>Order</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.State"><span class="badge">E</span>State</a>
                </li>
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fchannel%2fv1%2fgenesis.proto">ibcgo/core/channel/v1/genesis.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.GenesisState"><span class="badge">M</span>GenesisState</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.PacketSequence"><span class="badge">M</span>PacketSequence</a>
                </li>
              
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fchannel%2fv1%2fquery.proto">ibcgo/core/channel/v1/query.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryChannelClientStateRequest"><span class="badge">M</span>QueryChannelClientStateRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryChannelClientStateResponse"><span class="badge">M</span>QueryChannelClientStateResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryChannelConsensusStateRequest"><span class="badge">M</span>QueryChannelConsensusStateRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryChannelConsensusStateResponse"><span class="badge">M</span>QueryChannelConsensusStateResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryChannelRequest"><span class="badge">M</span>QueryChannelRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryChannelResponse"><span class="badge">M</span>QueryChannelResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryChannelsRequest"><span class="badge">M</span>QueryChannelsRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryChannelsResponse"><span class="badge">M</span>QueryChannelsResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryConnectionChannelsRequest"><span class="badge">M</span>QueryConnectionChannelsRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryConnectionChannelsResponse"><span class="badge">M</span>QueryConnectionChannelsResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryNextSequenceReceiveRequest"><span class="badge">M</span>QueryNextSequenceReceiveRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryNextSequenceReceiveResponse"><span class="badge">M</span>QueryNextSequenceReceiveResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryPacketAcknowledgementRequest"><span class="badge">M</span>QueryPacketAcknowledgementRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryPacketAcknowledgementResponse"><span class="badge">M</span>QueryPacketAcknowledgementResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryPacketAcknowledgementsRequest"><span class="badge">M</span>QueryPacketAcknowledgementsRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryPacketAcknowledgementsResponse"><span class="badge">M</span>QueryPacketAcknowledgementsResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryPacketCommitmentRequest"><span class="badge">M</span>QueryPacketCommitmentRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryPacketCommitmentResponse"><span class="badge">M</span>QueryPacketCommitmentResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryPacketCommitmentsRequest"><span class="badge">M</span>QueryPacketCommitmentsRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryPacketCommitmentsResponse"><span class="badge">M</span>QueryPacketCommitmentsResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryPacketReceiptRequest"><span class="badge">M</span>QueryPacketReceiptRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryPacketReceiptResponse"><span class="badge">M</span>QueryPacketReceiptResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryUnreceivedAcksRequest"><span class="badge">M</span>QueryUnreceivedAcksRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryUnreceivedAcksResponse"><span class="badge">M</span>QueryUnreceivedAcksResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryUnreceivedPacketsRequest"><span class="badge">M</span>QueryUnreceivedPacketsRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.QueryUnreceivedPacketsResponse"><span class="badge">M</span>QueryUnreceivedPacketsResponse</a>
                </li>
              
              
              
              
                <li>
                  <a href="#ibcgo.core.channel.v1.Query"><span class="badge">S</span>Query</a>
                </li>
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fchannel%2fv1%2ftx.proto">ibcgo/core/channel/v1/tx.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgAcknowledgement"><span class="badge">M</span>MsgAcknowledgement</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgAcknowledgementResponse"><span class="badge">M</span>MsgAcknowledgementResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelCloseConfirm"><span class="badge">M</span>MsgChannelCloseConfirm</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelCloseConfirmResponse"><span class="badge">M</span>MsgChannelCloseConfirmResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelCloseInit"><span class="badge">M</span>MsgChannelCloseInit</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelCloseInitResponse"><span class="badge">M</span>MsgChannelCloseInitResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelOpenAck"><span class="badge">M</span>MsgChannelOpenAck</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelOpenAckResponse"><span class="badge">M</span>MsgChannelOpenAckResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelOpenConfirm"><span class="badge">M</span>MsgChannelOpenConfirm</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelOpenConfirmResponse"><span class="badge">M</span>MsgChannelOpenConfirmResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelOpenInit"><span class="badge">M</span>MsgChannelOpenInit</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelOpenInitResponse"><span class="badge">M</span>MsgChannelOpenInitResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelOpenTry"><span class="badge">M</span>MsgChannelOpenTry</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgChannelOpenTryResponse"><span class="badge">M</span>MsgChannelOpenTryResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgRecvPacket"><span class="badge">M</span>MsgRecvPacket</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgRecvPacketResponse"><span class="badge">M</span>MsgRecvPacketResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgTimeout"><span class="badge">M</span>MsgTimeout</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgTimeoutOnClose"><span class="badge">M</span>MsgTimeoutOnClose</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgTimeoutOnCloseResponse"><span class="badge">M</span>MsgTimeoutOnCloseResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.channel.v1.MsgTimeoutResponse"><span class="badge">M</span>MsgTimeoutResponse</a>
                </li>
              
              
              
              
                <li>
                  <a href="#ibcgo.core.channel.v1.Msg"><span class="badge">S</span>Msg</a>
                </li>
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fclient%2fv1%2fgenesis.proto">ibcgo/core/client/v1/genesis.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.client.v1.GenesisMetadata"><span class="badge">M</span>GenesisMetadata</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.GenesisState"><span class="badge">M</span>GenesisState</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.IdentifiedGenesisMetadata"><span class="badge">M</span>IdentifiedGenesisMetadata</a>
                </li>
              
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fclient%2fv1%2fquery.proto">ibcgo/core/client/v1/query.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.client.v1.QueryClientParamsRequest"><span class="badge">M</span>QueryClientParamsRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.QueryClientParamsResponse"><span class="badge">M</span>QueryClientParamsResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.QueryClientStateRequest"><span class="badge">M</span>QueryClientStateRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.QueryClientStateResponse"><span class="badge">M</span>QueryClientStateResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.QueryClientStatesRequest"><span class="badge">M</span>QueryClientStatesRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.QueryClientStatesResponse"><span class="badge">M</span>QueryClientStatesResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.QueryConsensusStateRequest"><span class="badge">M</span>QueryConsensusStateRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.QueryConsensusStateResponse"><span class="badge">M</span>QueryConsensusStateResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.QueryConsensusStatesRequest"><span class="badge">M</span>QueryConsensusStatesRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.QueryConsensusStatesResponse"><span class="badge">M</span>QueryConsensusStatesResponse</a>
                </li>
              
              
              
              
                <li>
                  <a href="#ibcgo.core.client.v1.Query"><span class="badge">S</span>Query</a>
                </li>
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fclient%2fv1%2ftx.proto">ibcgo/core/client/v1/tx.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.client.v1.MsgCreateClient"><span class="badge">M</span>MsgCreateClient</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.MsgCreateClientResponse"><span class="badge">M</span>MsgCreateClientResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.MsgSubmitMisbehaviour"><span class="badge">M</span>MsgSubmitMisbehaviour</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.MsgSubmitMisbehaviourResponse"><span class="badge">M</span>MsgSubmitMisbehaviourResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.MsgUpdateClient"><span class="badge">M</span>MsgUpdateClient</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.MsgUpdateClientResponse"><span class="badge">M</span>MsgUpdateClientResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.MsgUpgradeClient"><span class="badge">M</span>MsgUpgradeClient</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.client.v1.MsgUpgradeClientResponse"><span class="badge">M</span>MsgUpgradeClientResponse</a>
                </li>
              
              
              
              
                <li>
                  <a href="#ibcgo.core.client.v1.Msg"><span class="badge">S</span>Msg</a>
                </li>
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fcommitment%2fv1%2fcommitment.proto">ibcgo/core/commitment/v1/commitment.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.commitment.v1.MerklePath"><span class="badge">M</span>MerklePath</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.commitment.v1.MerklePrefix"><span class="badge">M</span>MerklePrefix</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.commitment.v1.MerkleProof"><span class="badge">M</span>MerkleProof</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.commitment.v1.MerkleRoot"><span class="badge">M</span>MerkleRoot</a>
                </li>
              
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fconnection%2fv1%2fconnection.proto">ibcgo/core/connection/v1/connection.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.ClientPaths"><span class="badge">M</span>ClientPaths</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.ConnectionEnd"><span class="badge">M</span>ConnectionEnd</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.ConnectionPaths"><span class="badge">M</span>ConnectionPaths</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.Counterparty"><span class="badge">M</span>Counterparty</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.IdentifiedConnection"><span class="badge">M</span>IdentifiedConnection</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.Version"><span class="badge">M</span>Version</a>
                </li>
              
              
                <li>
                  <a href="#ibcgo.core.connection.v1.State"><span class="badge">E</span>State</a>
                </li>
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fconnection%2fv1%2fgenesis.proto">ibcgo/core/connection/v1/genesis.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.GenesisState"><span class="badge">M</span>GenesisState</a>
                </li>
              
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fconnection%2fv1%2fquery.proto">ibcgo/core/connection/v1/query.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.QueryClientConnectionsRequest"><span class="badge">M</span>QueryClientConnectionsRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.QueryClientConnectionsResponse"><span class="badge">M</span>QueryClientConnectionsResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.QueryConnectionClientStateRequest"><span class="badge">M</span>QueryConnectionClientStateRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.QueryConnectionClientStateResponse"><span class="badge">M</span>QueryConnectionClientStateResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.QueryConnectionConsensusStateRequest"><span class="badge">M</span>QueryConnectionConsensusStateRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.QueryConnectionConsensusStateResponse"><span class="badge">M</span>QueryConnectionConsensusStateResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.QueryConnectionRequest"><span class="badge">M</span>QueryConnectionRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.QueryConnectionResponse"><span class="badge">M</span>QueryConnectionResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.QueryConnectionsRequest"><span class="badge">M</span>QueryConnectionsRequest</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.QueryConnectionsResponse"><span class="badge">M</span>QueryConnectionsResponse</a>
                </li>
              
              
              
              
                <li>
                  <a href="#ibcgo.core.connection.v1.Query"><span class="badge">S</span>Query</a>
                </li>
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2fconnection%2fv1%2ftx.proto">ibcgo/core/connection/v1/tx.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.MsgConnectionOpenAck"><span class="badge">M</span>MsgConnectionOpenAck</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.MsgConnectionOpenAckResponse"><span class="badge">M</span>MsgConnectionOpenAckResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.MsgConnectionOpenConfirm"><span class="badge">M</span>MsgConnectionOpenConfirm</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.MsgConnectionOpenConfirmResponse"><span class="badge">M</span>MsgConnectionOpenConfirmResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.MsgConnectionOpenInit"><span class="badge">M</span>MsgConnectionOpenInit</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.MsgConnectionOpenInitResponse"><span class="badge">M</span>MsgConnectionOpenInitResponse</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.MsgConnectionOpenTry"><span class="badge">M</span>MsgConnectionOpenTry</a>
                </li>
              
                <li>
                  <a href="#ibcgo.core.connection.v1.MsgConnectionOpenTryResponse"><span class="badge">M</span>MsgConnectionOpenTryResponse</a>
                </li>
              
              
              
              
                <li>
                  <a href="#ibcgo.core.connection.v1.Msg"><span class="badge">S</span>Msg</a>
                </li>
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2fcore%2ftypes%2fv1%2fgenesis.proto">ibcgo/core/types/v1/genesis.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.core.types.v1.GenesisState"><span class="badge">M</span>GenesisState</a>
                </li>
              
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2flightclients%2flocalhost%2fv1%2flocalhost.proto">ibcgo/lightclients/localhost/v1/localhost.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.lightclients.localhost.v1.ClientState"><span class="badge">M</span>ClientState</a>
                </li>
              
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2flightclients%2fsolomachine%2fv1%2fsolomachine.proto">ibcgo/lightclients/solomachine/v1/solomachine.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.ChannelStateData"><span class="badge">M</span>ChannelStateData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.ClientState"><span class="badge">M</span>ClientState</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.ClientStateData"><span class="badge">M</span>ClientStateData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.ConnectionStateData"><span class="badge">M</span>ConnectionStateData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.ConsensusState"><span class="badge">M</span>ConsensusState</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.ConsensusStateData"><span class="badge">M</span>ConsensusStateData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.Header"><span class="badge">M</span>Header</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.HeaderData"><span class="badge">M</span>HeaderData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.Misbehaviour"><span class="badge">M</span>Misbehaviour</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.NextSequenceRecvData"><span class="badge">M</span>NextSequenceRecvData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.PacketAcknowledgementData"><span class="badge">M</span>PacketAcknowledgementData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.PacketCommitmentData"><span class="badge">M</span>PacketCommitmentData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.PacketReceiptAbsenceData"><span class="badge">M</span>PacketReceiptAbsenceData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.SignBytes"><span class="badge">M</span>SignBytes</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.SignatureAndData"><span class="badge">M</span>SignatureAndData</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.TimestampedSignatureData"><span class="badge">M</span>TimestampedSignatureData</a>
                </li>
              
              
                <li>
                  <a href="#ibcgo.lightclients.solomachine.v1.DataType"><span class="badge">E</span>DataType</a>
                </li>
              
              
              
            </ul>
          </li>
        
          
          <li>
            <a href="#ibcgo%2flightclients%2ftendermint%2fv1%2ftendermint.proto">ibcgo/lightclients/tendermint/v1/tendermint.proto</a>
            <ul>
              
                <li>
                  <a href="#ibcgo.lightclients.tendermint.v1.ClientState"><span class="badge">M</span>ClientState</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.tendermint.v1.ConsensusState"><span class="badge">M</span>ConsensusState</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.tendermint.v1.Fraction"><span class="badge">M</span>Fraction</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.tendermint.v1.Header"><span class="badge">M</span>Header</a>
                </li>
              
                <li>
                  <a href="#ibcgo.lightclients.tendermint.v1.Misbehaviour"><span class="badge">M</span>Misbehaviour</a>
                </li>
              
              
              
              
            </ul>
          </li>
        
        <li><a href="#scalar-value-types">Scalar Value Types</a></li>
      </ul>
    </div>

    
      
      <div class="file-heading">
        <h2 id="ibcgo/apps/transfer/v1/transfer.proto">ibcgo/apps/transfer/v1/transfer.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.apps.transfer.v1.DenomTrace">DenomTrace</h3>
        <p>DenomTrace contains the base denomination for ICS20 fungible tokens and the</p><p>source tracing information path.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>path</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>path defines the chain of port/channel identifiers used for tracing the
source of the fungible token. </p></td>
                </tr>
              
                <tr>
                  <td>base_denom</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>base denomination of the relayed fungible token. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.apps.transfer.v1.FungibleTokenPacketData">FungibleTokenPacketData</h3>
        <p>FungibleTokenPacketData defines a struct for the packet payload</p><p>See FungibleTokenPacketData spec:</p><p>https://github.com/cosmos/ics/tree/master/spec/ics-020-fungible-token-transfer#data-structures</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>denom</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the token denomination to be transferred </p></td>
                </tr>
              
                <tr>
                  <td>amount</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>the token amount to be transferred </p></td>
                </tr>
              
                <tr>
                  <td>sender</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the sender address </p></td>
                </tr>
              
                <tr>
                  <td>receiver</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the recipient address on the destination chain </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.apps.transfer.v1.Params">Params</h3>
        <p>Params defines the set of IBC transfer parameters.</p><p>NOTE: To prevent a single token from being transferred, set the</p><p>TransfersEnabled parameter to true and then set the bank module's SendEnabled</p><p>parameter for the denomination to false.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>send_enabled</td>
                  <td><a href="#bool">bool</a></td>
                  <td></td>
                  <td><p>send_enabled enables or disables all cross-chain token transfers from this
chain. </p></td>
                </tr>
              
                <tr>
                  <td>receive_enabled</td>
                  <td><a href="#bool">bool</a></td>
                  <td></td>
                  <td><p>receive_enabled enables or disables all cross-chain token transfers to this
chain. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/apps/transfer/v1/genesis.proto">ibcgo/apps/transfer/v1/genesis.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.apps.transfer.v1.GenesisState">GenesisState</h3>
        <p>GenesisState defines the ibc-transfer genesis state</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>denom_traces</td>
                  <td><a href="#ibcgo.apps.transfer.v1.DenomTrace">DenomTrace</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>params</td>
                  <td><a href="#ibcgo.apps.transfer.v1.Params">Params</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/apps/transfer/v1/query.proto">ibcgo/apps/transfer/v1/query.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.apps.transfer.v1.QueryDenomTraceRequest">QueryDenomTraceRequest</h3>
        <p>QueryDenomTraceRequest is the request type for the Query/DenomTrace RPC</p><p>method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>hash</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>hash (in hex format) of the denomination trace information. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.apps.transfer.v1.QueryDenomTraceResponse">QueryDenomTraceResponse</h3>
        <p>QueryDenomTraceResponse is the response type for the Query/DenomTrace RPC</p><p>method.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>denom_trace</td>
                  <td><a href="#ibcgo.apps.transfer.v1.DenomTrace">DenomTrace</a></td>
                  <td></td>
                  <td><p>denom_trace returns the requested denomination trace information. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.apps.transfer.v1.QueryDenomTracesRequest">QueryDenomTracesRequest</h3>
        <p>QueryConnectionsRequest is the request type for the Query/DenomTraces RPC</p><p>method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageRequest">cosmos.base.query.v1beta1.PageRequest</a></td>
                  <td></td>
                  <td><p>pagination defines an optional pagination for the request. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.apps.transfer.v1.QueryDenomTracesResponse">QueryDenomTracesResponse</h3>
        <p>QueryConnectionsResponse is the response type for the Query/DenomTraces RPC</p><p>method.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>denom_traces</td>
                  <td><a href="#ibcgo.apps.transfer.v1.DenomTrace">DenomTrace</a></td>
                  <td>repeated</td>
                  <td><p>denom_traces returns all denominations trace information. </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageResponse">cosmos.base.query.v1beta1.PageResponse</a></td>
                  <td></td>
                  <td><p>pagination defines the pagination in the response. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.apps.transfer.v1.QueryParamsRequest">QueryParamsRequest</h3>
        <p>QueryParamsRequest is the request type for the Query/Params RPC method.</p>

        

        
      
        <h3 id="ibcgo.apps.transfer.v1.QueryParamsResponse">QueryParamsResponse</h3>
        <p>QueryParamsResponse is the response type for the Query/Params RPC method.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>params</td>
                  <td><a href="#ibcgo.apps.transfer.v1.Params">Params</a></td>
                  <td></td>
                  <td><p>params defines the parameters of the module. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
        <h3 id="ibcgo.apps.transfer.v1.Query">Query</h3>
        <p>Query provides defines the gRPC querier service.</p>
        <table class="enum-table">
          <thead>
            <tr><td>Method Name</td><td>Request Type</td><td>Response Type</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>DenomTrace</td>
                <td><a href="#ibcgo.apps.transfer.v1.QueryDenomTraceRequest">QueryDenomTraceRequest</a></td>
                <td><a href="#ibcgo.apps.transfer.v1.QueryDenomTraceResponse">QueryDenomTraceResponse</a></td>
                <td><p>DenomTrace queries a denomination trace information.</p></td>
              </tr>
            
              <tr>
                <td>DenomTraces</td>
                <td><a href="#ibcgo.apps.transfer.v1.QueryDenomTracesRequest">QueryDenomTracesRequest</a></td>
                <td><a href="#ibcgo.apps.transfer.v1.QueryDenomTracesResponse">QueryDenomTracesResponse</a></td>
                <td><p>DenomTraces queries all denomination traces.</p></td>
              </tr>
            
              <tr>
                <td>Params</td>
                <td><a href="#ibcgo.apps.transfer.v1.QueryParamsRequest">QueryParamsRequest</a></td>
                <td><a href="#ibcgo.apps.transfer.v1.QueryParamsResponse">QueryParamsResponse</a></td>
                <td><p>Params queries all parameters of the ibc-transfer module.</p></td>
              </tr>
            
          </tbody>
        </table>

        
          
          
          <h4>Methods with HTTP bindings</h4>
          <table>
            <thead>
              <tr>
                <td>Method Name</td>
                <td>Method</td>
                <td>Pattern</td>
                <td>Body</td>
              </tr>
            </thead>
            <tbody>
            
              
              
              <tr>
                <td>DenomTrace</td>
                <td>GET</td>
                <td>/ibc/apps/transfer/v1/denom_traces/{hash}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>DenomTraces</td>
                <td>GET</td>
                <td>/ibc/apps/transfer/v1/denom_traces</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>Params</td>
                <td>GET</td>
                <td>/ibc/apps/transfer/v1/params</td>
                <td></td>
              </tr>
              
            
            </tbody>
          </table>
          
        
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/client/v1/client.proto">ibcgo/core/client/v1/client.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.client.v1.ClientConsensusStates">ClientConsensusStates</h3>
        <p>ClientConsensusStates defines all the stored consensus states for a given</p><p>client.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client identifier </p></td>
                </tr>
              
                <tr>
                  <td>consensus_states</td>
                  <td><a href="#ibcgo.core.client.v1.ConsensusStateWithHeight">ConsensusStateWithHeight</a></td>
                  <td>repeated</td>
                  <td><p>consensus states and their heights associated with the client </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.ClientUpdateProposal">ClientUpdateProposal</h3>
        <p>ClientUpdateProposal is a governance proposal. If it passes, the substitute</p><p>client's consensus states starting from the 'initial height' are copied over</p><p>to the subjects client state. The proposal handler may fail if the subject</p><p>and the substitute do not match in client and chain parameters (with</p><p>exception to latest height, frozen height, and chain-id). The updated client</p><p>must also be valid (cannot be expired).</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>title</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the title of the update proposal </p></td>
                </tr>
              
                <tr>
                  <td>description</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the description of the proposal </p></td>
                </tr>
              
                <tr>
                  <td>subject_client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the client identifier for the client to be updated if the proposal passes </p></td>
                </tr>
              
                <tr>
                  <td>substitute_client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the substitute client identifier for the client standing in for the subject
client </p></td>
                </tr>
              
                <tr>
                  <td>initial_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">Height</a></td>
                  <td></td>
                  <td><p>the intital height to copy consensus states from the substitute to the
subject </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.ConsensusStateWithHeight">ConsensusStateWithHeight</h3>
        <p>ConsensusStateWithHeight defines a consensus state with an additional height</p><p>field.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">Height</a></td>
                  <td></td>
                  <td><p>consensus state height </p></td>
                </tr>
              
                <tr>
                  <td>consensus_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>consensus state </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.Height">Height</h3>
        <p>Height is a monotonically increasing data type</p><p>that can be compared against another Height for the purposes of updating and</p><p>freezing clients</p><p>Normally the RevisionHeight is incremented at each height while keeping</p><p>RevisionNumber the same. However some consensus algorithms may choose to</p><p>reset the height in certain conditions e.g. hard forks, state-machine</p><p>breaking changes In these cases, the RevisionNumber is incremented so that</p><p>height continues to be monitonically increasing even as the RevisionHeight</p><p>gets reset</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>revision_number</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>the revision that the client is currently on </p></td>
                </tr>
              
                <tr>
                  <td>revision_height</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>the height within the given revision </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.IdentifiedClientState">IdentifiedClientState</h3>
        <p>IdentifiedClientState defines a client state with an additional client</p><p>identifier field.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client identifier </p></td>
                </tr>
              
                <tr>
                  <td>client_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>client state </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.Params">Params</h3>
        <p>Params defines the set of IBC light client parameters.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>allowed_clients</td>
                  <td><a href="#string">string</a></td>
                  <td>repeated</td>
                  <td><p>allowed_clients defines the list of allowed client state types. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/apps/transfer/v1/tx.proto">ibcgo/apps/transfer/v1/tx.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.apps.transfer.v1.MsgTransfer">MsgTransfer</h3>
        <p>MsgTransfer defines a msg to transfer fungible tokens (i.e Coins) between</p><p>ICS20 enabled chains. See ICS Spec here:</p><p>https://github.com/cosmos/ics/tree/master/spec/ics-020-fungible-token-transfer#data-structures</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>source_port</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the port on which the packet will be sent </p></td>
                </tr>
              
                <tr>
                  <td>source_channel</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the channel by which the packet will be sent </p></td>
                </tr>
              
                <tr>
                  <td>token</td>
                  <td><a href="#cosmos.base.v1beta1.Coin">cosmos.base.v1beta1.Coin</a></td>
                  <td></td>
                  <td><p>the tokens to be transferred </p></td>
                </tr>
              
                <tr>
                  <td>sender</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the sender address </p></td>
                </tr>
              
                <tr>
                  <td>receiver</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>the recipient address on the destination chain </p></td>
                </tr>
              
                <tr>
                  <td>timeout_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>Timeout height relative to the current block height.
The timeout is disabled when set to 0. </p></td>
                </tr>
              
                <tr>
                  <td>timeout_timestamp</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>Timeout timestamp (in nanoseconds) relative to the current block timestamp.
The timeout is disabled when set to 0. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.apps.transfer.v1.MsgTransferResponse">MsgTransferResponse</h3>
        <p>MsgTransferResponse defines the Msg/Transfer response type.</p>

        

        
      

      

      

      
        <h3 id="ibcgo.apps.transfer.v1.Msg">Msg</h3>
        <p>Msg defines the ibc/transfer Msg service.</p>
        <table class="enum-table">
          <thead>
            <tr><td>Method Name</td><td>Request Type</td><td>Response Type</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>Transfer</td>
                <td><a href="#ibcgo.apps.transfer.v1.MsgTransfer">MsgTransfer</a></td>
                <td><a href="#ibcgo.apps.transfer.v1.MsgTransferResponse">MsgTransferResponse</a></td>
                <td><p>Transfer defines a rpc handler method for MsgTransfer.</p></td>
              </tr>
            
          </tbody>
        </table>

        
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/channel/v1/channel.proto">ibcgo/core/channel/v1/channel.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.channel.v1.Acknowledgement">Acknowledgement</h3>
        <p>Acknowledgement is the recommended acknowledgement format to be used by</p><p>app-specific protocols.</p><p>NOTE: The field numbers 21 and 22 were explicitly chosen to avoid accidental</p><p>conflicts with other protobuf message formats used for acknowledgements.</p><p>The first byte of any message with this format will be the non-ASCII values</p><p>`0xaa` (result) or `0xb2` (error). Implemented as defined by ICS:</p><p>https://github.com/cosmos/ics/tree/master/spec/ics-004-channel-and-packet-semantics#acknowledgement-envelope</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>result</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>error</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.Channel">Channel</h3>
        <p>Channel defines pipeline for exactly-once packet delivery between specific</p><p>modules on separate blockchains, which has at least one end capable of</p><p>sending packets and one end capable of receiving packets.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>state</td>
                  <td><a href="#ibcgo.core.channel.v1.State">State</a></td>
                  <td></td>
                  <td><p>current state of the channel end </p></td>
                </tr>
              
                <tr>
                  <td>ordering</td>
                  <td><a href="#ibcgo.core.channel.v1.Order">Order</a></td>
                  <td></td>
                  <td><p>whether the channel is ordered or unordered </p></td>
                </tr>
              
                <tr>
                  <td>counterparty</td>
                  <td><a href="#ibcgo.core.channel.v1.Counterparty">Counterparty</a></td>
                  <td></td>
                  <td><p>counterparty channel end </p></td>
                </tr>
              
                <tr>
                  <td>connection_hops</td>
                  <td><a href="#string">string</a></td>
                  <td>repeated</td>
                  <td><p>list of connection identifiers, in order, along which packets sent on
this channel will travel </p></td>
                </tr>
              
                <tr>
                  <td>version</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>opaque channel version, which is agreed upon during the handshake </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.Counterparty">Counterparty</h3>
        <p>Counterparty defines a channel end counterparty</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port on the counterparty chain which owns the other end of the channel. </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel end on the counterparty chain </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.IdentifiedChannel">IdentifiedChannel</h3>
        <p>IdentifiedChannel defines a channel with additional port and channel</p><p>identifier fields.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>state</td>
                  <td><a href="#ibcgo.core.channel.v1.State">State</a></td>
                  <td></td>
                  <td><p>current state of the channel end </p></td>
                </tr>
              
                <tr>
                  <td>ordering</td>
                  <td><a href="#ibcgo.core.channel.v1.Order">Order</a></td>
                  <td></td>
                  <td><p>whether the channel is ordered or unordered </p></td>
                </tr>
              
                <tr>
                  <td>counterparty</td>
                  <td><a href="#ibcgo.core.channel.v1.Counterparty">Counterparty</a></td>
                  <td></td>
                  <td><p>counterparty channel end </p></td>
                </tr>
              
                <tr>
                  <td>connection_hops</td>
                  <td><a href="#string">string</a></td>
                  <td>repeated</td>
                  <td><p>list of connection identifiers, in order, along which packets sent on
this channel will travel </p></td>
                </tr>
              
                <tr>
                  <td>version</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>opaque channel version, which is agreed upon during the handshake </p></td>
                </tr>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.Packet">Packet</h3>
        <p>Packet defines a type that carries data across different chains through IBC</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>number corresponds to the order of sends and receives, where a Packet
with an earlier sequence number must be sent and received before a Packet
with a later sequence number. </p></td>
                </tr>
              
                <tr>
                  <td>source_port</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>identifies the port on the sending chain. </p></td>
                </tr>
              
                <tr>
                  <td>source_channel</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>identifies the channel end on the sending chain. </p></td>
                </tr>
              
                <tr>
                  <td>destination_port</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>identifies the port on the receiving chain. </p></td>
                </tr>
              
                <tr>
                  <td>destination_channel</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>identifies the channel end on the receiving chain. </p></td>
                </tr>
              
                <tr>
                  <td>data</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>actual opaque bytes transferred directly to the application module </p></td>
                </tr>
              
                <tr>
                  <td>timeout_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>block height after which the packet times out </p></td>
                </tr>
              
                <tr>
                  <td>timeout_timestamp</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>block timestamp (in nanoseconds) after which the packet times out </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.PacketState">PacketState</h3>
        <p>PacketState defines the generic type necessary to retrieve and store</p><p>packet commitments, acknowledgements, and receipts.</p><p>Caller is responsible for knowing the context necessary to interpret this</p><p>state as a commitment, acknowledgement, or a receipt.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel port identifier. </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier. </p></td>
                </tr>
              
                <tr>
                  <td>sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>packet sequence. </p></td>
                </tr>
              
                <tr>
                  <td>data</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>embedded data that represents packet state. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      
        <h3 id="ibcgo.core.channel.v1.Order">Order</h3>
        <p>Order defines if a channel is ORDERED or UNORDERED</p>
        <table class="enum-table">
          <thead>
            <tr><td>Name</td><td>Number</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>ORDER_NONE_UNSPECIFIED</td>
                <td>0</td>
                <td><p>zero-value for channel ordering</p></td>
              </tr>
            
              <tr>
                <td>ORDER_UNORDERED</td>
                <td>1</td>
                <td><p>packets can be delivered in any order, which may differ from the order in
which they were sent.</p></td>
              </tr>
            
              <tr>
                <td>ORDER_ORDERED</td>
                <td>2</td>
                <td><p>packets are delivered exactly in the order which they were sent</p></td>
              </tr>
            
          </tbody>
        </table>
      
        <h3 id="ibcgo.core.channel.v1.State">State</h3>
        <p>State defines if a channel is in one of the following states:</p><p>CLOSED, INIT, TRYOPEN, OPEN or UNINITIALIZED.</p>
        <table class="enum-table">
          <thead>
            <tr><td>Name</td><td>Number</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>STATE_UNINITIALIZED_UNSPECIFIED</td>
                <td>0</td>
                <td><p>Default State</p></td>
              </tr>
            
              <tr>
                <td>STATE_INIT</td>
                <td>1</td>
                <td><p>A channel has just started the opening handshake.</p></td>
              </tr>
            
              <tr>
                <td>STATE_TRYOPEN</td>
                <td>2</td>
                <td><p>A channel has acknowledged the handshake step on the counterparty chain.</p></td>
              </tr>
            
              <tr>
                <td>STATE_OPEN</td>
                <td>3</td>
                <td><p>A channel has completed the handshake. Open channels are
ready to send and receive packets.</p></td>
              </tr>
            
              <tr>
                <td>STATE_CLOSED</td>
                <td>4</td>
                <td><p>A channel has been closed and can no longer be used to send or receive
packets.</p></td>
              </tr>
            
          </tbody>
        </table>
      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/channel/v1/genesis.proto">ibcgo/core/channel/v1/genesis.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.channel.v1.GenesisState">GenesisState</h3>
        <p>GenesisState defines the ibc channel submodule's genesis state.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>channels</td>
                  <td><a href="#ibcgo.core.channel.v1.IdentifiedChannel">IdentifiedChannel</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>acknowledgements</td>
                  <td><a href="#ibcgo.core.channel.v1.PacketState">PacketState</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>commitments</td>
                  <td><a href="#ibcgo.core.channel.v1.PacketState">PacketState</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>receipts</td>
                  <td><a href="#ibcgo.core.channel.v1.PacketState">PacketState</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>send_sequences</td>
                  <td><a href="#ibcgo.core.channel.v1.PacketSequence">PacketSequence</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>recv_sequences</td>
                  <td><a href="#ibcgo.core.channel.v1.PacketSequence">PacketSequence</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>ack_sequences</td>
                  <td><a href="#ibcgo.core.channel.v1.PacketSequence">PacketSequence</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>next_channel_sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>the sequence for the next generated channel identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.PacketSequence">PacketSequence</h3>
        <p>PacketSequence defines the genesis type necessary to retrieve and store</p><p>next send and receive sequences.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/channel/v1/query.proto">ibcgo/core/channel/v1/query.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.channel.v1.QueryChannelClientStateRequest">QueryChannelClientStateRequest</h3>
        <p>QueryChannelClientStateRequest is the request type for the Query/ClientState</p><p>RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryChannelClientStateResponse">QueryChannelClientStateResponse</h3>
        <p>QueryChannelClientStateResponse is the Response type for the</p><p>Query/QueryChannelClientState RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>identified_client_state</td>
                  <td><a href="#ibcgo.core.client.v1.IdentifiedClientState">ibcgo.core.client.v1.IdentifiedClientState</a></td>
                  <td></td>
                  <td><p>client state associated with the channel </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryChannelConsensusStateRequest">QueryChannelConsensusStateRequest</h3>
        <p>QueryChannelConsensusStateRequest is the request type for the</p><p>Query/ConsensusState RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>revision_number</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>revision number of the consensus state </p></td>
                </tr>
              
                <tr>
                  <td>revision_height</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>revision height of the consensus state </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryChannelConsensusStateResponse">QueryChannelConsensusStateResponse</h3>
        <p>QueryChannelClientStateResponse is the Response type for the</p><p>Query/QueryChannelClientState RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>consensus_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>consensus state associated with the channel </p></td>
                </tr>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client ID associated with the consensus state </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryChannelRequest">QueryChannelRequest</h3>
        <p>QueryChannelRequest is the request type for the Query/Channel RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryChannelResponse">QueryChannelResponse</h3>
        <p>QueryChannelResponse is the response type for the Query/Channel RPC method.</p><p>Besides the Channel end, it includes a proof and the height from which the</p><p>proof was retrieved.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>channel</td>
                  <td><a href="#ibcgo.core.channel.v1.Channel">Channel</a></td>
                  <td></td>
                  <td><p>channel associated with the request identifiers </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryChannelsRequest">QueryChannelsRequest</h3>
        <p>QueryChannelsRequest is the request type for the Query/Channels RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageRequest">cosmos.base.query.v1beta1.PageRequest</a></td>
                  <td></td>
                  <td><p>pagination request </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryChannelsResponse">QueryChannelsResponse</h3>
        <p>QueryChannelsResponse is the response type for the Query/Channels RPC method.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>channels</td>
                  <td><a href="#ibcgo.core.channel.v1.IdentifiedChannel">IdentifiedChannel</a></td>
                  <td>repeated</td>
                  <td><p>list of stored channels of the chain. </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageResponse">cosmos.base.query.v1beta1.PageResponse</a></td>
                  <td></td>
                  <td><p>pagination response </p></td>
                </tr>
              
                <tr>
                  <td>height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>query block height </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryConnectionChannelsRequest">QueryConnectionChannelsRequest</h3>
        <p>QueryConnectionChannelsRequest is the request type for the</p><p>Query/QueryConnectionChannels RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>connection</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>connection unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageRequest">cosmos.base.query.v1beta1.PageRequest</a></td>
                  <td></td>
                  <td><p>pagination request </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryConnectionChannelsResponse">QueryConnectionChannelsResponse</h3>
        <p>QueryConnectionChannelsResponse is the Response type for the</p><p>Query/QueryConnectionChannels RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>channels</td>
                  <td><a href="#ibcgo.core.channel.v1.IdentifiedChannel">IdentifiedChannel</a></td>
                  <td>repeated</td>
                  <td><p>list of channels associated with a connection. </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageResponse">cosmos.base.query.v1beta1.PageResponse</a></td>
                  <td></td>
                  <td><p>pagination response </p></td>
                </tr>
              
                <tr>
                  <td>height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>query block height </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryNextSequenceReceiveRequest">QueryNextSequenceReceiveRequest</h3>
        <p>QueryNextSequenceReceiveRequest is the request type for the</p><p>Query/QueryNextSequenceReceiveRequest RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryNextSequenceReceiveResponse">QueryNextSequenceReceiveResponse</h3>
        <p>QuerySequenceResponse is the request type for the</p><p>Query/QueryNextSequenceReceiveResponse RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>next_sequence_receive</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>next sequence receive number </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryPacketAcknowledgementRequest">QueryPacketAcknowledgementRequest</h3>
        <p>QueryPacketAcknowledgementRequest is the request type for the</p><p>Query/PacketAcknowledgement RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>packet sequence </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryPacketAcknowledgementResponse">QueryPacketAcknowledgementResponse</h3>
        <p>QueryPacketAcknowledgementResponse defines the client query response for a</p><p>packet which also includes a proof and the height from which the</p><p>proof was retrieved</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>acknowledgement</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>packet associated with the request fields </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryPacketAcknowledgementsRequest">QueryPacketAcknowledgementsRequest</h3>
        <p>QueryPacketAcknowledgementsRequest is the request type for the</p><p>Query/QueryPacketCommitments RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageRequest">cosmos.base.query.v1beta1.PageRequest</a></td>
                  <td></td>
                  <td><p>pagination request </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryPacketAcknowledgementsResponse">QueryPacketAcknowledgementsResponse</h3>
        <p>QueryPacketAcknowledgemetsResponse is the request type for the</p><p>Query/QueryPacketAcknowledgements RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>acknowledgements</td>
                  <td><a href="#ibcgo.core.channel.v1.PacketState">PacketState</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageResponse">cosmos.base.query.v1beta1.PageResponse</a></td>
                  <td></td>
                  <td><p>pagination response </p></td>
                </tr>
              
                <tr>
                  <td>height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>query block height </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryPacketCommitmentRequest">QueryPacketCommitmentRequest</h3>
        <p>QueryPacketCommitmentRequest is the request type for the</p><p>Query/PacketCommitment RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>packet sequence </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryPacketCommitmentResponse">QueryPacketCommitmentResponse</h3>
        <p>QueryPacketCommitmentResponse defines the client query response for a packet</p><p>which also includes a proof and the height from which the proof was</p><p>retrieved</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>commitment</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>packet associated with the request fields </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryPacketCommitmentsRequest">QueryPacketCommitmentsRequest</h3>
        <p>QueryPacketCommitmentsRequest is the request type for the</p><p>Query/QueryPacketCommitments RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageRequest">cosmos.base.query.v1beta1.PageRequest</a></td>
                  <td></td>
                  <td><p>pagination request </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryPacketCommitmentsResponse">QueryPacketCommitmentsResponse</h3>
        <p>QueryPacketCommitmentsResponse is the request type for the</p><p>Query/QueryPacketCommitments RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>commitments</td>
                  <td><a href="#ibcgo.core.channel.v1.PacketState">PacketState</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageResponse">cosmos.base.query.v1beta1.PageResponse</a></td>
                  <td></td>
                  <td><p>pagination response </p></td>
                </tr>
              
                <tr>
                  <td>height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>query block height </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryPacketReceiptRequest">QueryPacketReceiptRequest</h3>
        <p>QueryPacketReceiptRequest is the request type for the</p><p>Query/PacketReceipt RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>packet sequence </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryPacketReceiptResponse">QueryPacketReceiptResponse</h3>
        <p>QueryPacketReceiptResponse defines the client query response for a packet</p><p>receipt which also includes a proof, and the height from which the proof was</p><p>retrieved</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>received</td>
                  <td><a href="#bool">bool</a></td>
                  <td></td>
                  <td><p>success flag for if receipt exists </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryUnreceivedAcksRequest">QueryUnreceivedAcksRequest</h3>
        <p>QueryUnreceivedAcks is the request type for the</p><p>Query/UnreceivedAcks RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>packet_ack_sequences</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td>repeated</td>
                  <td><p>list of acknowledgement sequences </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryUnreceivedAcksResponse">QueryUnreceivedAcksResponse</h3>
        <p>QueryUnreceivedAcksResponse is the response type for the</p><p>Query/UnreceivedAcks RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>sequences</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td>repeated</td>
                  <td><p>list of unreceived acknowledgement sequences </p></td>
                </tr>
              
                <tr>
                  <td>height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>query block height </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryUnreceivedPacketsRequest">QueryUnreceivedPacketsRequest</h3>
        <p>QueryUnreceivedPacketsRequest is the request type for the</p><p>Query/UnreceivedPackets RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>port unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>channel unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>packet_commitment_sequences</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td>repeated</td>
                  <td><p>list of packet sequences </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.QueryUnreceivedPacketsResponse">QueryUnreceivedPacketsResponse</h3>
        <p>QueryUnreceivedPacketsResponse is the response type for the</p><p>Query/UnreceivedPacketCommitments RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>sequences</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td>repeated</td>
                  <td><p>list of unreceived packet sequences </p></td>
                </tr>
              
                <tr>
                  <td>height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>query block height </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
        <h3 id="ibcgo.core.channel.v1.Query">Query</h3>
        <p>Query provides defines the gRPC querier service</p>
        <table class="enum-table">
          <thead>
            <tr><td>Method Name</td><td>Request Type</td><td>Response Type</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>Channel</td>
                <td><a href="#ibcgo.core.channel.v1.QueryChannelRequest">QueryChannelRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryChannelResponse">QueryChannelResponse</a></td>
                <td><p>Channel queries an IBC Channel.</p></td>
              </tr>
            
              <tr>
                <td>Channels</td>
                <td><a href="#ibcgo.core.channel.v1.QueryChannelsRequest">QueryChannelsRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryChannelsResponse">QueryChannelsResponse</a></td>
                <td><p>Channels queries all the IBC channels of a chain.</p></td>
              </tr>
            
              <tr>
                <td>ConnectionChannels</td>
                <td><a href="#ibcgo.core.channel.v1.QueryConnectionChannelsRequest">QueryConnectionChannelsRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryConnectionChannelsResponse">QueryConnectionChannelsResponse</a></td>
                <td><p>ConnectionChannels queries all the channels associated with a connection
end.</p></td>
              </tr>
            
              <tr>
                <td>ChannelClientState</td>
                <td><a href="#ibcgo.core.channel.v1.QueryChannelClientStateRequest">QueryChannelClientStateRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryChannelClientStateResponse">QueryChannelClientStateResponse</a></td>
                <td><p>ChannelClientState queries for the client state for the channel associated
with the provided channel identifiers.</p></td>
              </tr>
            
              <tr>
                <td>ChannelConsensusState</td>
                <td><a href="#ibcgo.core.channel.v1.QueryChannelConsensusStateRequest">QueryChannelConsensusStateRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryChannelConsensusStateResponse">QueryChannelConsensusStateResponse</a></td>
                <td><p>ChannelConsensusState queries for the consensus state for the channel
associated with the provided channel identifiers.</p></td>
              </tr>
            
              <tr>
                <td>PacketCommitment</td>
                <td><a href="#ibcgo.core.channel.v1.QueryPacketCommitmentRequest">QueryPacketCommitmentRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryPacketCommitmentResponse">QueryPacketCommitmentResponse</a></td>
                <td><p>PacketCommitment queries a stored packet commitment hash.</p></td>
              </tr>
            
              <tr>
                <td>PacketCommitments</td>
                <td><a href="#ibcgo.core.channel.v1.QueryPacketCommitmentsRequest">QueryPacketCommitmentsRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryPacketCommitmentsResponse">QueryPacketCommitmentsResponse</a></td>
                <td><p>PacketCommitments returns all the packet commitments hashes associated
with a channel.</p></td>
              </tr>
            
              <tr>
                <td>PacketReceipt</td>
                <td><a href="#ibcgo.core.channel.v1.QueryPacketReceiptRequest">QueryPacketReceiptRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryPacketReceiptResponse">QueryPacketReceiptResponse</a></td>
                <td><p>PacketReceipt queries if a given packet sequence has been received on the
queried chain</p></td>
              </tr>
            
              <tr>
                <td>PacketAcknowledgement</td>
                <td><a href="#ibcgo.core.channel.v1.QueryPacketAcknowledgementRequest">QueryPacketAcknowledgementRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryPacketAcknowledgementResponse">QueryPacketAcknowledgementResponse</a></td>
                <td><p>PacketAcknowledgement queries a stored packet acknowledgement hash.</p></td>
              </tr>
            
              <tr>
                <td>PacketAcknowledgements</td>
                <td><a href="#ibcgo.core.channel.v1.QueryPacketAcknowledgementsRequest">QueryPacketAcknowledgementsRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryPacketAcknowledgementsResponse">QueryPacketAcknowledgementsResponse</a></td>
                <td><p>PacketAcknowledgements returns all the packet acknowledgements associated
with a channel.</p></td>
              </tr>
            
              <tr>
                <td>UnreceivedPackets</td>
                <td><a href="#ibcgo.core.channel.v1.QueryUnreceivedPacketsRequest">QueryUnreceivedPacketsRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryUnreceivedPacketsResponse">QueryUnreceivedPacketsResponse</a></td>
                <td><p>UnreceivedPackets returns all the unreceived IBC packets associated with a
channel and sequences.</p></td>
              </tr>
            
              <tr>
                <td>UnreceivedAcks</td>
                <td><a href="#ibcgo.core.channel.v1.QueryUnreceivedAcksRequest">QueryUnreceivedAcksRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryUnreceivedAcksResponse">QueryUnreceivedAcksResponse</a></td>
                <td><p>UnreceivedAcks returns all the unreceived IBC acknowledgements associated
with a channel and sequences.</p></td>
              </tr>
            
              <tr>
                <td>NextSequenceReceive</td>
                <td><a href="#ibcgo.core.channel.v1.QueryNextSequenceReceiveRequest">QueryNextSequenceReceiveRequest</a></td>
                <td><a href="#ibcgo.core.channel.v1.QueryNextSequenceReceiveResponse">QueryNextSequenceReceiveResponse</a></td>
                <td><p>NextSequenceReceive returns the next receive sequence for a given channel.</p></td>
              </tr>
            
          </tbody>
        </table>

        
          
          
          <h4>Methods with HTTP bindings</h4>
          <table>
            <thead>
              <tr>
                <td>Method Name</td>
                <td>Method</td>
                <td>Pattern</td>
                <td>Body</td>
              </tr>
            </thead>
            <tbody>
            
              
              
              <tr>
                <td>Channel</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>Channels</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>ConnectionChannels</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/connections/{connection}/channels</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>ChannelClientState</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/client_state</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>ChannelConsensusState</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/consensus_state/revision/{revision_number}/height/{revision_height}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>PacketCommitment</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_commitments/{sequence}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>PacketCommitments</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_commitments</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>PacketReceipt</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_receipts/{sequence}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>PacketAcknowledgement</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_acks/{sequence}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>PacketAcknowledgements</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_acknowledgements</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>UnreceivedPackets</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_commitments/{packet_commitment_sequences}/unreceived_packets</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>UnreceivedAcks</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/packet_commitments/{packet_ack_sequences}/unreceived_acks</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>NextSequenceReceive</td>
                <td>GET</td>
                <td>/ibc/core/channel/v1/channels/{channel_id}/ports/{port_id}/next_sequence</td>
                <td></td>
              </tr>
              
            
            </tbody>
          </table>
          
        
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/channel/v1/tx.proto">ibcgo/core/channel/v1/tx.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.channel.v1.MsgAcknowledgement">MsgAcknowledgement</h3>
        <p>MsgAcknowledgement receives incoming IBC acknowledgement</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>packet</td>
                  <td><a href="#ibcgo.core.channel.v1.Packet">Packet</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>acknowledgement</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_acked</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.MsgAcknowledgementResponse">MsgAcknowledgementResponse</h3>
        <p>MsgAcknowledgementResponse defines the Msg/Acknowledgement response type.</p>

        

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelCloseConfirm">MsgChannelCloseConfirm</h3>
        <p>MsgChannelCloseConfirm defines a msg sent by a Relayer to Chain B</p><p>to acknowledge the change of channel state to CLOSED on Chain A.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_init</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelCloseConfirmResponse">MsgChannelCloseConfirmResponse</h3>
        <p>MsgChannelCloseConfirmResponse defines the Msg/ChannelCloseConfirm response</p><p>type.</p>

        

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelCloseInit">MsgChannelCloseInit</h3>
        <p>MsgChannelCloseInit defines a msg sent by a Relayer to Chain A</p><p>to close a channel with Chain B.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelCloseInitResponse">MsgChannelCloseInitResponse</h3>
        <p>MsgChannelCloseInitResponse defines the Msg/ChannelCloseInit response type.</p>

        

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelOpenAck">MsgChannelOpenAck</h3>
        <p>MsgChannelOpenAck defines a msg sent by a Relayer to Chain A to acknowledge</p><p>the change of channel state to TRYOPEN on Chain B.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>counterparty_channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>counterparty_version</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_try</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelOpenAckResponse">MsgChannelOpenAckResponse</h3>
        <p>MsgChannelOpenAckResponse defines the Msg/ChannelOpenAck response type.</p>

        

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelOpenConfirm">MsgChannelOpenConfirm</h3>
        <p>MsgChannelOpenConfirm defines a msg sent by a Relayer to Chain B to</p><p>acknowledge the change of channel state to OPEN on Chain A.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_ack</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelOpenConfirmResponse">MsgChannelOpenConfirmResponse</h3>
        <p>MsgChannelOpenConfirmResponse defines the Msg/ChannelOpenConfirm response</p><p>type.</p>

        

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelOpenInit">MsgChannelOpenInit</h3>
        <p>MsgChannelOpenInit defines an sdk.Msg to initialize a channel handshake. It</p><p>is called by a relayer on Chain A.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>channel</td>
                  <td><a href="#ibcgo.core.channel.v1.Channel">Channel</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelOpenInitResponse">MsgChannelOpenInitResponse</h3>
        <p>MsgChannelOpenInitResponse defines the Msg/ChannelOpenInit response type.</p>

        

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelOpenTry">MsgChannelOpenTry</h3>
        <p>MsgChannelOpenInit defines a msg sent by a Relayer to try to open a channel</p><p>on Chain B.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>port_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>previous_channel_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>in the case of crossing hello&#39;s, when both chains call OpenInit, we need
the channel identifier of the previous channel in state INIT </p></td>
                </tr>
              
                <tr>
                  <td>channel</td>
                  <td><a href="#ibcgo.core.channel.v1.Channel">Channel</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>counterparty_version</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_init</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.MsgChannelOpenTryResponse">MsgChannelOpenTryResponse</h3>
        <p>MsgChannelOpenTryResponse defines the Msg/ChannelOpenTry response type.</p>

        

        
      
        <h3 id="ibcgo.core.channel.v1.MsgRecvPacket">MsgRecvPacket</h3>
        <p>MsgRecvPacket receives incoming IBC packet</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>packet</td>
                  <td><a href="#ibcgo.core.channel.v1.Packet">Packet</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_commitment</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.MsgRecvPacketResponse">MsgRecvPacketResponse</h3>
        <p>MsgRecvPacketResponse defines the Msg/RecvPacket response type.</p>

        

        
      
        <h3 id="ibcgo.core.channel.v1.MsgTimeout">MsgTimeout</h3>
        <p>MsgTimeout receives timed-out packet</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>packet</td>
                  <td><a href="#ibcgo.core.channel.v1.Packet">Packet</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_unreceived</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>next_sequence_recv</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.MsgTimeoutOnClose">MsgTimeoutOnClose</h3>
        <p>MsgTimeoutOnClose timed-out packet upon counterparty channel closure.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>packet</td>
                  <td><a href="#ibcgo.core.channel.v1.Packet">Packet</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_unreceived</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_close</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>next_sequence_recv</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.channel.v1.MsgTimeoutOnCloseResponse">MsgTimeoutOnCloseResponse</h3>
        <p>MsgTimeoutOnCloseResponse defines the Msg/TimeoutOnClose response type.</p>

        

        
      
        <h3 id="ibcgo.core.channel.v1.MsgTimeoutResponse">MsgTimeoutResponse</h3>
        <p>MsgTimeoutResponse defines the Msg/Timeout response type.</p>

        

        
      

      

      

      
        <h3 id="ibcgo.core.channel.v1.Msg">Msg</h3>
        <p>Msg defines the ibc/channel Msg service.</p>
        <table class="enum-table">
          <thead>
            <tr><td>Method Name</td><td>Request Type</td><td>Response Type</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>ChannelOpenInit</td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelOpenInit">MsgChannelOpenInit</a></td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelOpenInitResponse">MsgChannelOpenInitResponse</a></td>
                <td><p>ChannelOpenInit defines a rpc handler method for MsgChannelOpenInit.</p></td>
              </tr>
            
              <tr>
                <td>ChannelOpenTry</td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelOpenTry">MsgChannelOpenTry</a></td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelOpenTryResponse">MsgChannelOpenTryResponse</a></td>
                <td><p>ChannelOpenTry defines a rpc handler method for MsgChannelOpenTry.</p></td>
              </tr>
            
              <tr>
                <td>ChannelOpenAck</td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelOpenAck">MsgChannelOpenAck</a></td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelOpenAckResponse">MsgChannelOpenAckResponse</a></td>
                <td><p>ChannelOpenAck defines a rpc handler method for MsgChannelOpenAck.</p></td>
              </tr>
            
              <tr>
                <td>ChannelOpenConfirm</td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelOpenConfirm">MsgChannelOpenConfirm</a></td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelOpenConfirmResponse">MsgChannelOpenConfirmResponse</a></td>
                <td><p>ChannelOpenConfirm defines a rpc handler method for MsgChannelOpenConfirm.</p></td>
              </tr>
            
              <tr>
                <td>ChannelCloseInit</td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelCloseInit">MsgChannelCloseInit</a></td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelCloseInitResponse">MsgChannelCloseInitResponse</a></td>
                <td><p>ChannelCloseInit defines a rpc handler method for MsgChannelCloseInit.</p></td>
              </tr>
            
              <tr>
                <td>ChannelCloseConfirm</td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelCloseConfirm">MsgChannelCloseConfirm</a></td>
                <td><a href="#ibcgo.core.channel.v1.MsgChannelCloseConfirmResponse">MsgChannelCloseConfirmResponse</a></td>
                <td><p>ChannelCloseConfirm defines a rpc handler method for
MsgChannelCloseConfirm.</p></td>
              </tr>
            
              <tr>
                <td>RecvPacket</td>
                <td><a href="#ibcgo.core.channel.v1.MsgRecvPacket">MsgRecvPacket</a></td>
                <td><a href="#ibcgo.core.channel.v1.MsgRecvPacketResponse">MsgRecvPacketResponse</a></td>
                <td><p>RecvPacket defines a rpc handler method for MsgRecvPacket.</p></td>
              </tr>
            
              <tr>
                <td>Timeout</td>
                <td><a href="#ibcgo.core.channel.v1.MsgTimeout">MsgTimeout</a></td>
                <td><a href="#ibcgo.core.channel.v1.MsgTimeoutResponse">MsgTimeoutResponse</a></td>
                <td><p>Timeout defines a rpc handler method for MsgTimeout.</p></td>
              </tr>
            
              <tr>
                <td>TimeoutOnClose</td>
                <td><a href="#ibcgo.core.channel.v1.MsgTimeoutOnClose">MsgTimeoutOnClose</a></td>
                <td><a href="#ibcgo.core.channel.v1.MsgTimeoutOnCloseResponse">MsgTimeoutOnCloseResponse</a></td>
                <td><p>TimeoutOnClose defines a rpc handler method for MsgTimeoutOnClose.</p></td>
              </tr>
            
              <tr>
                <td>Acknowledgement</td>
                <td><a href="#ibcgo.core.channel.v1.MsgAcknowledgement">MsgAcknowledgement</a></td>
                <td><a href="#ibcgo.core.channel.v1.MsgAcknowledgementResponse">MsgAcknowledgementResponse</a></td>
                <td><p>Acknowledgement defines a rpc handler method for MsgAcknowledgement.</p></td>
              </tr>
            
          </tbody>
        </table>

        
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/client/v1/genesis.proto">ibcgo/core/client/v1/genesis.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.client.v1.GenesisMetadata">GenesisMetadata</h3>
        <p>GenesisMetadata defines the genesis type for metadata that clients may return</p><p>with ExportMetadata</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>key</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>store key of metadata without clientID-prefix </p></td>
                </tr>
              
                <tr>
                  <td>value</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>metadata value </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.GenesisState">GenesisState</h3>
        <p>GenesisState defines the ibc client submodule's genesis state.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>clients</td>
                  <td><a href="#ibcgo.core.client.v1.IdentifiedClientState">IdentifiedClientState</a></td>
                  <td>repeated</td>
                  <td><p>client states with their corresponding identifiers </p></td>
                </tr>
              
                <tr>
                  <td>clients_consensus</td>
                  <td><a href="#ibcgo.core.client.v1.ClientConsensusStates">ClientConsensusStates</a></td>
                  <td>repeated</td>
                  <td><p>consensus states from each client </p></td>
                </tr>
              
                <tr>
                  <td>clients_metadata</td>
                  <td><a href="#ibcgo.core.client.v1.IdentifiedGenesisMetadata">IdentifiedGenesisMetadata</a></td>
                  <td>repeated</td>
                  <td><p>metadata from each client </p></td>
                </tr>
              
                <tr>
                  <td>params</td>
                  <td><a href="#ibcgo.core.client.v1.Params">Params</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>create_localhost</td>
                  <td><a href="#bool">bool</a></td>
                  <td></td>
                  <td><p>create localhost on initialization </p></td>
                </tr>
              
                <tr>
                  <td>next_client_sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>the sequence for the next generated client identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.IdentifiedGenesisMetadata">IdentifiedGenesisMetadata</h3>
        <p>IdentifiedGenesisMetadata has the client metadata with the corresponding</p><p>client id.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>client_metadata</td>
                  <td><a href="#ibcgo.core.client.v1.GenesisMetadata">GenesisMetadata</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/client/v1/query.proto">ibcgo/core/client/v1/query.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.client.v1.QueryClientParamsRequest">QueryClientParamsRequest</h3>
        <p>QueryClientParamsRequest is the request type for the Query/ClientParams RPC</p><p>method.</p>

        

        
      
        <h3 id="ibcgo.core.client.v1.QueryClientParamsResponse">QueryClientParamsResponse</h3>
        <p>QueryClientParamsResponse is the response type for the Query/ClientParams RPC</p><p>method.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>params</td>
                  <td><a href="#ibcgo.core.client.v1.Params">Params</a></td>
                  <td></td>
                  <td><p>params defines the parameters of the module. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.QueryClientStateRequest">QueryClientStateRequest</h3>
        <p>QueryClientStateRequest is the request type for the Query/ClientState RPC</p><p>method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client state unique identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.QueryClientStateResponse">QueryClientStateResponse</h3>
        <p>QueryClientStateResponse is the response type for the Query/ClientState RPC</p><p>method. Besides the client state, it includes a proof and the height from</p><p>which the proof was retrieved.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>client state associated with the request identifier </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.QueryClientStatesRequest">QueryClientStatesRequest</h3>
        <p>QueryClientStatesRequest is the request type for the Query/ClientStates RPC</p><p>method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageRequest">cosmos.base.query.v1beta1.PageRequest</a></td>
                  <td></td>
                  <td><p>pagination request </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.QueryClientStatesResponse">QueryClientStatesResponse</h3>
        <p>QueryClientStatesResponse is the response type for the Query/ClientStates RPC</p><p>method.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_states</td>
                  <td><a href="#ibcgo.core.client.v1.IdentifiedClientState">IdentifiedClientState</a></td>
                  <td>repeated</td>
                  <td><p>list of stored ClientStates of the chain. </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageResponse">cosmos.base.query.v1beta1.PageResponse</a></td>
                  <td></td>
                  <td><p>pagination response </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.QueryConsensusStateRequest">QueryConsensusStateRequest</h3>
        <p>QueryConsensusStateRequest is the request type for the Query/ConsensusState</p><p>RPC method. Besides the consensus state, it includes a proof and the height</p><p>from which the proof was retrieved.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client identifier </p></td>
                </tr>
              
                <tr>
                  <td>revision_number</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>consensus state revision number </p></td>
                </tr>
              
                <tr>
                  <td>revision_height</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>consensus state revision height </p></td>
                </tr>
              
                <tr>
                  <td>latest_height</td>
                  <td><a href="#bool">bool</a></td>
                  <td></td>
                  <td><p>latest_height overrrides the height field and queries the latest stored
ConsensusState </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.QueryConsensusStateResponse">QueryConsensusStateResponse</h3>
        <p>QueryConsensusStateResponse is the response type for the Query/ConsensusState</p><p>RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>consensus_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>consensus state associated with the client identifier at the given height </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.QueryConsensusStatesRequest">QueryConsensusStatesRequest</h3>
        <p>QueryConsensusStatesRequest is the request type for the Query/ConsensusStates</p><p>RPC method.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client identifier </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageRequest">cosmos.base.query.v1beta1.PageRequest</a></td>
                  <td></td>
                  <td><p>pagination request </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.QueryConsensusStatesResponse">QueryConsensusStatesResponse</h3>
        <p>QueryConsensusStatesResponse is the response type for the</p><p>Query/ConsensusStates RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>consensus_states</td>
                  <td><a href="#ibcgo.core.client.v1.ConsensusStateWithHeight">ConsensusStateWithHeight</a></td>
                  <td>repeated</td>
                  <td><p>consensus states associated with the identifier </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageResponse">cosmos.base.query.v1beta1.PageResponse</a></td>
                  <td></td>
                  <td><p>pagination response </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
        <h3 id="ibcgo.core.client.v1.Query">Query</h3>
        <p>Query provides defines the gRPC querier service</p>
        <table class="enum-table">
          <thead>
            <tr><td>Method Name</td><td>Request Type</td><td>Response Type</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>ClientState</td>
                <td><a href="#ibcgo.core.client.v1.QueryClientStateRequest">QueryClientStateRequest</a></td>
                <td><a href="#ibcgo.core.client.v1.QueryClientStateResponse">QueryClientStateResponse</a></td>
                <td><p>ClientState queries an IBC light client.</p></td>
              </tr>
            
              <tr>
                <td>ClientStates</td>
                <td><a href="#ibcgo.core.client.v1.QueryClientStatesRequest">QueryClientStatesRequest</a></td>
                <td><a href="#ibcgo.core.client.v1.QueryClientStatesResponse">QueryClientStatesResponse</a></td>
                <td><p>ClientStates queries all the IBC light clients of a chain.</p></td>
              </tr>
            
              <tr>
                <td>ConsensusState</td>
                <td><a href="#ibcgo.core.client.v1.QueryConsensusStateRequest">QueryConsensusStateRequest</a></td>
                <td><a href="#ibcgo.core.client.v1.QueryConsensusStateResponse">QueryConsensusStateResponse</a></td>
                <td><p>ConsensusState queries a consensus state associated with a client state at
a given height.</p></td>
              </tr>
            
              <tr>
                <td>ConsensusStates</td>
                <td><a href="#ibcgo.core.client.v1.QueryConsensusStatesRequest">QueryConsensusStatesRequest</a></td>
                <td><a href="#ibcgo.core.client.v1.QueryConsensusStatesResponse">QueryConsensusStatesResponse</a></td>
                <td><p>ConsensusStates queries all the consensus state associated with a given
client.</p></td>
              </tr>
            
              <tr>
                <td>ClientParams</td>
                <td><a href="#ibcgo.core.client.v1.QueryClientParamsRequest">QueryClientParamsRequest</a></td>
                <td><a href="#ibcgo.core.client.v1.QueryClientParamsResponse">QueryClientParamsResponse</a></td>
                <td><p>ClientParams queries all parameters of the ibc client.</p></td>
              </tr>
            
          </tbody>
        </table>

        
          
          
          <h4>Methods with HTTP bindings</h4>
          <table>
            <thead>
              <tr>
                <td>Method Name</td>
                <td>Method</td>
                <td>Pattern</td>
                <td>Body</td>
              </tr>
            </thead>
            <tbody>
            
              
              
              <tr>
                <td>ClientState</td>
                <td>GET</td>
                <td>/ibc/core/client/v1/client_states/{client_id}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>ClientStates</td>
                <td>GET</td>
                <td>/ibc/core/client/v1/client_states</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>ConsensusState</td>
                <td>GET</td>
                <td>/ibc/core/client/v1/consensus_states/{client_id}/revision/{revision_number}/height/{revision_height}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>ConsensusStates</td>
                <td>GET</td>
                <td>/ibc/core/client/v1/consensus_states/{client_id}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>ClientParams</td>
                <td>GET</td>
                <td>/ibc/client/v1/params</td>
                <td></td>
              </tr>
              
            
            </tbody>
          </table>
          
        
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/client/v1/tx.proto">ibcgo/core/client/v1/tx.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.client.v1.MsgCreateClient">MsgCreateClient</h3>
        <p>MsgCreateClient defines a message to create an IBC client</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>light client state </p></td>
                </tr>
              
                <tr>
                  <td>consensus_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>consensus state associated with the client that corresponds to a given
height. </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>signer address </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.MsgCreateClientResponse">MsgCreateClientResponse</h3>
        <p>MsgCreateClientResponse defines the Msg/CreateClient response type.</p>

        

        
      
        <h3 id="ibcgo.core.client.v1.MsgSubmitMisbehaviour">MsgSubmitMisbehaviour</h3>
        <p>MsgSubmitMisbehaviour defines an sdk.Msg type that submits Evidence for</p><p>light client misbehaviour.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>misbehaviour</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>misbehaviour used for freezing the light client </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>signer address </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.MsgSubmitMisbehaviourResponse">MsgSubmitMisbehaviourResponse</h3>
        <p>MsgSubmitMisbehaviourResponse defines the Msg/SubmitMisbehaviour response</p><p>type.</p>

        

        
      
        <h3 id="ibcgo.core.client.v1.MsgUpdateClient">MsgUpdateClient</h3>
        <p>MsgUpdateClient defines an sdk.Msg to update a IBC client state using</p><p>the given header.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>header</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>header to update the light client </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>signer address </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.MsgUpdateClientResponse">MsgUpdateClientResponse</h3>
        <p>MsgUpdateClientResponse defines the Msg/UpdateClient response type.</p>

        

        
      
        <h3 id="ibcgo.core.client.v1.MsgUpgradeClient">MsgUpgradeClient</h3>
        <p>MsgUpgradeClient defines an sdk.Msg to upgrade an IBC client to a new client</p><p>state</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>client_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>upgraded client state </p></td>
                </tr>
              
                <tr>
                  <td>consensus_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>upgraded consensus state, only contains enough information to serve as a
basis of trust in update logic </p></td>
                </tr>
              
                <tr>
                  <td>proof_upgrade_client</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>proof that old chain committed to new client </p></td>
                </tr>
              
                <tr>
                  <td>proof_upgrade_consensus_state</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>proof that old chain committed to new consensus state </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>signer address </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.client.v1.MsgUpgradeClientResponse">MsgUpgradeClientResponse</h3>
        <p>MsgUpgradeClientResponse defines the Msg/UpgradeClient response type.</p>

        

        
      

      

      

      
        <h3 id="ibcgo.core.client.v1.Msg">Msg</h3>
        <p>Msg defines the ibc/client Msg service.</p>
        <table class="enum-table">
          <thead>
            <tr><td>Method Name</td><td>Request Type</td><td>Response Type</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>CreateClient</td>
                <td><a href="#ibcgo.core.client.v1.MsgCreateClient">MsgCreateClient</a></td>
                <td><a href="#ibcgo.core.client.v1.MsgCreateClientResponse">MsgCreateClientResponse</a></td>
                <td><p>CreateClient defines a rpc handler method for MsgCreateClient.</p></td>
              </tr>
            
              <tr>
                <td>UpdateClient</td>
                <td><a href="#ibcgo.core.client.v1.MsgUpdateClient">MsgUpdateClient</a></td>
                <td><a href="#ibcgo.core.client.v1.MsgUpdateClientResponse">MsgUpdateClientResponse</a></td>
                <td><p>UpdateClient defines a rpc handler method for MsgUpdateClient.</p></td>
              </tr>
            
              <tr>
                <td>UpgradeClient</td>
                <td><a href="#ibcgo.core.client.v1.MsgUpgradeClient">MsgUpgradeClient</a></td>
                <td><a href="#ibcgo.core.client.v1.MsgUpgradeClientResponse">MsgUpgradeClientResponse</a></td>
                <td><p>UpgradeClient defines a rpc handler method for MsgUpgradeClient.</p></td>
              </tr>
            
              <tr>
                <td>SubmitMisbehaviour</td>
                <td><a href="#ibcgo.core.client.v1.MsgSubmitMisbehaviour">MsgSubmitMisbehaviour</a></td>
                <td><a href="#ibcgo.core.client.v1.MsgSubmitMisbehaviourResponse">MsgSubmitMisbehaviourResponse</a></td>
                <td><p>SubmitMisbehaviour defines a rpc handler method for MsgSubmitMisbehaviour.</p></td>
              </tr>
            
          </tbody>
        </table>

        
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/commitment/v1/commitment.proto">ibcgo/core/commitment/v1/commitment.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.commitment.v1.MerklePath">MerklePath</h3>
        <p>MerklePath is the path used to verify commitment proofs, which can be an</p><p>arbitrary structured object (defined by a commitment type).</p><p>MerklePath is represented from root-to-leaf</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>key_path</td>
                  <td><a href="#string">string</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.commitment.v1.MerklePrefix">MerklePrefix</h3>
        <p>MerklePrefix is merkle path prefixed to the key.</p><p>The constructed key from the Path and the key will be append(Path.KeyPath,</p><p>append(Path.KeyPrefix, key...))</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>key_prefix</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.commitment.v1.MerkleProof">MerkleProof</h3>
        <p>MerkleProof is a wrapper type over a chain of CommitmentProofs.</p><p>It demonstrates membership or non-membership for an element or set of</p><p>elements, verifiable in conjunction with a known commitment root. Proofs</p><p>should be succinct.</p><p>MerkleProofs are ordered from leaf-to-root</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>proofs</td>
                  <td><a href="#ics23.CommitmentProof">ics23.CommitmentProof</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.commitment.v1.MerkleRoot">MerkleRoot</h3>
        <p>MerkleRoot defines a merkle root hash.</p><p>In the Cosmos SDK, the AppHash of a block header becomes the root.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>hash</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/connection/v1/connection.proto">ibcgo/core/connection/v1/connection.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.connection.v1.ClientPaths">ClientPaths</h3>
        <p>ClientPaths define all the connection paths for a client state.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>paths</td>
                  <td><a href="#string">string</a></td>
                  <td>repeated</td>
                  <td><p>list of connection paths </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.ConnectionEnd">ConnectionEnd</h3>
        <p>ConnectionEnd defines a stateful object on a chain connected to another</p><p>separate one.</p><p>NOTE: there must only be 2 defined ConnectionEnds to establish</p><p>a connection between two chains.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client associated with this connection. </p></td>
                </tr>
              
                <tr>
                  <td>versions</td>
                  <td><a href="#ibcgo.core.connection.v1.Version">Version</a></td>
                  <td>repeated</td>
                  <td><p>IBC version which can be utilised to determine encodings or protocols for
channels or packets utilising this connection. </p></td>
                </tr>
              
                <tr>
                  <td>state</td>
                  <td><a href="#ibcgo.core.connection.v1.State">State</a></td>
                  <td></td>
                  <td><p>current state of the connection end. </p></td>
                </tr>
              
                <tr>
                  <td>counterparty</td>
                  <td><a href="#ibcgo.core.connection.v1.Counterparty">Counterparty</a></td>
                  <td></td>
                  <td><p>counterparty chain associated with this connection. </p></td>
                </tr>
              
                <tr>
                  <td>delay_period</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>delay period that must pass before a consensus state can be used for
packet-verification NOTE: delay period logic is only implemented by some
clients. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.ConnectionPaths">ConnectionPaths</h3>
        <p>ConnectionPaths define all the connection paths for a given client state.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client state unique identifier </p></td>
                </tr>
              
                <tr>
                  <td>paths</td>
                  <td><a href="#string">string</a></td>
                  <td>repeated</td>
                  <td><p>list of connection paths </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.Counterparty">Counterparty</h3>
        <p>Counterparty defines the counterparty chain associated with a connection end.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>identifies the client on the counterparty chain associated with a given
connection. </p></td>
                </tr>
              
                <tr>
                  <td>connection_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>identifies the connection end on the counterparty chain associated with a
given connection. </p></td>
                </tr>
              
                <tr>
                  <td>prefix</td>
                  <td><a href="#ibcgo.core.commitment.v1.MerklePrefix">ibcgo.core.commitment.v1.MerklePrefix</a></td>
                  <td></td>
                  <td><p>commitment merkle prefix of the counterparty chain. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.IdentifiedConnection">IdentifiedConnection</h3>
        <p>IdentifiedConnection defines a connection with additional connection</p><p>identifier field.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>connection identifier. </p></td>
                </tr>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client associated with this connection. </p></td>
                </tr>
              
                <tr>
                  <td>versions</td>
                  <td><a href="#ibcgo.core.connection.v1.Version">Version</a></td>
                  <td>repeated</td>
                  <td><p>IBC version which can be utilised to determine encodings or protocols for
channels or packets utilising this connection </p></td>
                </tr>
              
                <tr>
                  <td>state</td>
                  <td><a href="#ibcgo.core.connection.v1.State">State</a></td>
                  <td></td>
                  <td><p>current state of the connection end. </p></td>
                </tr>
              
                <tr>
                  <td>counterparty</td>
                  <td><a href="#ibcgo.core.connection.v1.Counterparty">Counterparty</a></td>
                  <td></td>
                  <td><p>counterparty chain associated with this connection. </p></td>
                </tr>
              
                <tr>
                  <td>delay_period</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>delay period associated with this connection. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.Version">Version</h3>
        <p>Version defines the versioning scheme used to negotiate the IBC verison in</p><p>the connection handshake.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>identifier</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>unique version identifier </p></td>
                </tr>
              
                <tr>
                  <td>features</td>
                  <td><a href="#string">string</a></td>
                  <td>repeated</td>
                  <td><p>list of features compatible with the specified identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      
        <h3 id="ibcgo.core.connection.v1.State">State</h3>
        <p>State defines if a connection is in one of the following states:</p><p>INIT, TRYOPEN, OPEN or UNINITIALIZED.</p>
        <table class="enum-table">
          <thead>
            <tr><td>Name</td><td>Number</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>STATE_UNINITIALIZED_UNSPECIFIED</td>
                <td>0</td>
                <td><p>Default State</p></td>
              </tr>
            
              <tr>
                <td>STATE_INIT</td>
                <td>1</td>
                <td><p>A connection end has just started the opening handshake.</p></td>
              </tr>
            
              <tr>
                <td>STATE_TRYOPEN</td>
                <td>2</td>
                <td><p>A connection end has acknowledged the handshake step on the counterparty
chain.</p></td>
              </tr>
            
              <tr>
                <td>STATE_OPEN</td>
                <td>3</td>
                <td><p>A connection end has completed the handshake.</p></td>
              </tr>
            
          </tbody>
        </table>
      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/connection/v1/genesis.proto">ibcgo/core/connection/v1/genesis.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.connection.v1.GenesisState">GenesisState</h3>
        <p>GenesisState defines the ibc connection submodule's genesis state.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>connections</td>
                  <td><a href="#ibcgo.core.connection.v1.IdentifiedConnection">IdentifiedConnection</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>client_connection_paths</td>
                  <td><a href="#ibcgo.core.connection.v1.ConnectionPaths">ConnectionPaths</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>next_connection_sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>the sequence for the next generated connection identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/connection/v1/query.proto">ibcgo/core/connection/v1/query.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.connection.v1.QueryClientConnectionsRequest">QueryClientConnectionsRequest</h3>
        <p>QueryClientConnectionsRequest is the request type for the</p><p>Query/ClientConnections RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client identifier associated with a connection </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.QueryClientConnectionsResponse">QueryClientConnectionsResponse</h3>
        <p>QueryClientConnectionsResponse is the response type for the</p><p>Query/ClientConnections RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>connection_paths</td>
                  <td><a href="#string">string</a></td>
                  <td>repeated</td>
                  <td><p>slice of all the connection paths associated with a client. </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was generated </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.QueryConnectionClientStateRequest">QueryConnectionClientStateRequest</h3>
        <p>QueryConnectionClientStateRequest is the request type for the</p><p>Query/ConnectionClientState RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>connection_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>connection identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.QueryConnectionClientStateResponse">QueryConnectionClientStateResponse</h3>
        <p>QueryConnectionClientStateResponse is the response type for the</p><p>Query/ConnectionClientState RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>identified_client_state</td>
                  <td><a href="#ibcgo.core.client.v1.IdentifiedClientState">ibcgo.core.client.v1.IdentifiedClientState</a></td>
                  <td></td>
                  <td><p>client state associated with the channel </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.QueryConnectionConsensusStateRequest">QueryConnectionConsensusStateRequest</h3>
        <p>QueryConnectionConsensusStateRequest is the request type for the</p><p>Query/ConnectionConsensusState RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>connection_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>connection identifier </p></td>
                </tr>
              
                <tr>
                  <td>revision_number</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>revision_height</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.QueryConnectionConsensusStateResponse">QueryConnectionConsensusStateResponse</h3>
        <p>QueryConnectionConsensusStateResponse is the response type for the</p><p>Query/ConnectionConsensusState RPC method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>consensus_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>consensus state associated with the channel </p></td>
                </tr>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>client ID associated with the consensus state </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.QueryConnectionRequest">QueryConnectionRequest</h3>
        <p>QueryConnectionRequest is the request type for the Query/Connection RPC</p><p>method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>connection_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>connection unique identifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.QueryConnectionResponse">QueryConnectionResponse</h3>
        <p>QueryConnectionResponse is the response type for the Query/Connection RPC</p><p>method. Besides the connection end, it includes a proof and the height from</p><p>which the proof was retrieved.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>connection</td>
                  <td><a href="#ibcgo.core.connection.v1.ConnectionEnd">ConnectionEnd</a></td>
                  <td></td>
                  <td><p>connection associated with the request identifier </p></td>
                </tr>
              
                <tr>
                  <td>proof</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>merkle proof of existence </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>height at which the proof was retrieved </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.QueryConnectionsRequest">QueryConnectionsRequest</h3>
        <p>QueryConnectionsRequest is the request type for the Query/Connections RPC</p><p>method</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageRequest">cosmos.base.query.v1beta1.PageRequest</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.QueryConnectionsResponse">QueryConnectionsResponse</h3>
        <p>QueryConnectionsResponse is the response type for the Query/Connections RPC</p><p>method.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>connections</td>
                  <td><a href="#ibcgo.core.connection.v1.IdentifiedConnection">IdentifiedConnection</a></td>
                  <td>repeated</td>
                  <td><p>list of stored connections of the chain. </p></td>
                </tr>
              
                <tr>
                  <td>pagination</td>
                  <td><a href="#cosmos.base.query.v1beta1.PageResponse">cosmos.base.query.v1beta1.PageResponse</a></td>
                  <td></td>
                  <td><p>pagination response </p></td>
                </tr>
              
                <tr>
                  <td>height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>query block height </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
        <h3 id="ibcgo.core.connection.v1.Query">Query</h3>
        <p>Query provides defines the gRPC querier service</p>
        <table class="enum-table">
          <thead>
            <tr><td>Method Name</td><td>Request Type</td><td>Response Type</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>Connection</td>
                <td><a href="#ibcgo.core.connection.v1.QueryConnectionRequest">QueryConnectionRequest</a></td>
                <td><a href="#ibcgo.core.connection.v1.QueryConnectionResponse">QueryConnectionResponse</a></td>
                <td><p>Connection queries an IBC connection end.</p></td>
              </tr>
            
              <tr>
                <td>Connections</td>
                <td><a href="#ibcgo.core.connection.v1.QueryConnectionsRequest">QueryConnectionsRequest</a></td>
                <td><a href="#ibcgo.core.connection.v1.QueryConnectionsResponse">QueryConnectionsResponse</a></td>
                <td><p>Connections queries all the IBC connections of a chain.</p></td>
              </tr>
            
              <tr>
                <td>ClientConnections</td>
                <td><a href="#ibcgo.core.connection.v1.QueryClientConnectionsRequest">QueryClientConnectionsRequest</a></td>
                <td><a href="#ibcgo.core.connection.v1.QueryClientConnectionsResponse">QueryClientConnectionsResponse</a></td>
                <td><p>ClientConnections queries the connection paths associated with a client
state.</p></td>
              </tr>
            
              <tr>
                <td>ConnectionClientState</td>
                <td><a href="#ibcgo.core.connection.v1.QueryConnectionClientStateRequest">QueryConnectionClientStateRequest</a></td>
                <td><a href="#ibcgo.core.connection.v1.QueryConnectionClientStateResponse">QueryConnectionClientStateResponse</a></td>
                <td><p>ConnectionClientState queries the client state associated with the
connection.</p></td>
              </tr>
            
              <tr>
                <td>ConnectionConsensusState</td>
                <td><a href="#ibcgo.core.connection.v1.QueryConnectionConsensusStateRequest">QueryConnectionConsensusStateRequest</a></td>
                <td><a href="#ibcgo.core.connection.v1.QueryConnectionConsensusStateResponse">QueryConnectionConsensusStateResponse</a></td>
                <td><p>ConnectionConsensusState queries the consensus state associated with the
connection.</p></td>
              </tr>
            
          </tbody>
        </table>

        
          
          
          <h4>Methods with HTTP bindings</h4>
          <table>
            <thead>
              <tr>
                <td>Method Name</td>
                <td>Method</td>
                <td>Pattern</td>
                <td>Body</td>
              </tr>
            </thead>
            <tbody>
            
              
              
              <tr>
                <td>Connection</td>
                <td>GET</td>
                <td>/ibc/core/connection/v1/connections/{connection_id}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>Connections</td>
                <td>GET</td>
                <td>/ibc/core/connection/v1/connections</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>ClientConnections</td>
                <td>GET</td>
                <td>/ibc/core/connection/v1/client_connections/{client_id}</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>ConnectionClientState</td>
                <td>GET</td>
                <td>/ibc/core/connection/v1/connections/{connection_id}/client_state</td>
                <td></td>
              </tr>
              
            
              
              
              <tr>
                <td>ConnectionConsensusState</td>
                <td>GET</td>
                <td>/ibc/core/connection/v1/connections/{connection_id}/consensus_state/revision/{revision_number}/height/{revision_height}</td>
                <td></td>
              </tr>
              
            
            </tbody>
          </table>
          
        
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/connection/v1/tx.proto">ibcgo/core/connection/v1/tx.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.connection.v1.MsgConnectionOpenAck">MsgConnectionOpenAck</h3>
        <p>MsgConnectionOpenAck defines a msg sent by a Relayer to Chain A to</p><p>acknowledge the change of connection state to TRYOPEN on Chain B.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>connection_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>counterparty_connection_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>version</td>
                  <td><a href="#ibcgo.core.connection.v1.Version">Version</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>client_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_try</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>proof of the initialization the connection on Chain B: `UNITIALIZED -&gt;
TRYOPEN` </p></td>
                </tr>
              
                <tr>
                  <td>proof_client</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>proof of client state included in message </p></td>
                </tr>
              
                <tr>
                  <td>proof_consensus</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>proof of client consensus state </p></td>
                </tr>
              
                <tr>
                  <td>consensus_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.MsgConnectionOpenAckResponse">MsgConnectionOpenAckResponse</h3>
        <p>MsgConnectionOpenAckResponse defines the Msg/ConnectionOpenAck response type.</p>

        

        
      
        <h3 id="ibcgo.core.connection.v1.MsgConnectionOpenConfirm">MsgConnectionOpenConfirm</h3>
        <p>MsgConnectionOpenConfirm defines a msg sent by a Relayer to Chain B to</p><p>acknowledge the change of connection state to OPEN on Chain A.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>connection_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_ack</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>proof for the change of the connection state on Chain A: `INIT -&gt; OPEN` </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.MsgConnectionOpenConfirmResponse">MsgConnectionOpenConfirmResponse</h3>
        <p>MsgConnectionOpenConfirmResponse defines the Msg/ConnectionOpenConfirm</p><p>response type.</p>

        

        
      
        <h3 id="ibcgo.core.connection.v1.MsgConnectionOpenInit">MsgConnectionOpenInit</h3>
        <p>MsgConnectionOpenInit defines the msg sent by an account on Chain A to</p><p>initialize a connection with Chain B.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>counterparty</td>
                  <td><a href="#ibcgo.core.connection.v1.Counterparty">Counterparty</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>version</td>
                  <td><a href="#ibcgo.core.connection.v1.Version">Version</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>delay_period</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.MsgConnectionOpenInitResponse">MsgConnectionOpenInitResponse</h3>
        <p>MsgConnectionOpenInitResponse defines the Msg/ConnectionOpenInit response</p><p>type.</p>

        

        
      
        <h3 id="ibcgo.core.connection.v1.MsgConnectionOpenTry">MsgConnectionOpenTry</h3>
        <p>MsgConnectionOpenTry defines a msg sent by a Relayer to try to open a</p><p>connection on Chain B.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>previous_connection_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>in the case of crossing hello&#39;s, when both chains call OpenInit, we need
the connection identifier of the previous connection in state INIT </p></td>
                </tr>
              
                <tr>
                  <td>client_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>counterparty</td>
                  <td><a href="#ibcgo.core.connection.v1.Counterparty">Counterparty</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>delay_period</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>counterparty_versions</td>
                  <td><a href="#ibcgo.core.connection.v1.Version">Version</a></td>
                  <td>repeated</td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>proof_init</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>proof of the initialization the connection on Chain A: `UNITIALIZED -&gt;
INIT` </p></td>
                </tr>
              
                <tr>
                  <td>proof_client</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>proof of client state included in message </p></td>
                </tr>
              
                <tr>
                  <td>proof_consensus</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>proof of client consensus state </p></td>
                </tr>
              
                <tr>
                  <td>consensus_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signer</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.core.connection.v1.MsgConnectionOpenTryResponse">MsgConnectionOpenTryResponse</h3>
        <p>MsgConnectionOpenTryResponse defines the Msg/ConnectionOpenTry response type.</p>

        

        
      

      

      

      
        <h3 id="ibcgo.core.connection.v1.Msg">Msg</h3>
        <p>Msg defines the ibc/connection Msg service.</p>
        <table class="enum-table">
          <thead>
            <tr><td>Method Name</td><td>Request Type</td><td>Response Type</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>ConnectionOpenInit</td>
                <td><a href="#ibcgo.core.connection.v1.MsgConnectionOpenInit">MsgConnectionOpenInit</a></td>
                <td><a href="#ibcgo.core.connection.v1.MsgConnectionOpenInitResponse">MsgConnectionOpenInitResponse</a></td>
                <td><p>ConnectionOpenInit defines a rpc handler method for MsgConnectionOpenInit.</p></td>
              </tr>
            
              <tr>
                <td>ConnectionOpenTry</td>
                <td><a href="#ibcgo.core.connection.v1.MsgConnectionOpenTry">MsgConnectionOpenTry</a></td>
                <td><a href="#ibcgo.core.connection.v1.MsgConnectionOpenTryResponse">MsgConnectionOpenTryResponse</a></td>
                <td><p>ConnectionOpenTry defines a rpc handler method for MsgConnectionOpenTry.</p></td>
              </tr>
            
              <tr>
                <td>ConnectionOpenAck</td>
                <td><a href="#ibcgo.core.connection.v1.MsgConnectionOpenAck">MsgConnectionOpenAck</a></td>
                <td><a href="#ibcgo.core.connection.v1.MsgConnectionOpenAckResponse">MsgConnectionOpenAckResponse</a></td>
                <td><p>ConnectionOpenAck defines a rpc handler method for MsgConnectionOpenAck.</p></td>
              </tr>
            
              <tr>
                <td>ConnectionOpenConfirm</td>
                <td><a href="#ibcgo.core.connection.v1.MsgConnectionOpenConfirm">MsgConnectionOpenConfirm</a></td>
                <td><a href="#ibcgo.core.connection.v1.MsgConnectionOpenConfirmResponse">MsgConnectionOpenConfirmResponse</a></td>
                <td><p>ConnectionOpenConfirm defines a rpc handler method for
MsgConnectionOpenConfirm.</p></td>
              </tr>
            
          </tbody>
        </table>

        
    
      
      <div class="file-heading">
        <h2 id="ibcgo/core/types/v1/genesis.proto">ibcgo/core/types/v1/genesis.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.core.types.v1.GenesisState">GenesisState</h3>
        <p>GenesisState defines the ibc module's genesis state.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_genesis</td>
                  <td><a href="#ibcgo.core.client.v1.GenesisState">ibcgo.core.client.v1.GenesisState</a></td>
                  <td></td>
                  <td><p>ICS002 - Clients genesis state </p></td>
                </tr>
              
                <tr>
                  <td>connection_genesis</td>
                  <td><a href="#ibcgo.core.connection.v1.GenesisState">ibcgo.core.connection.v1.GenesisState</a></td>
                  <td></td>
                  <td><p>ICS003 - Connections genesis state </p></td>
                </tr>
              
                <tr>
                  <td>channel_genesis</td>
                  <td><a href="#ibcgo.core.channel.v1.GenesisState">ibcgo.core.channel.v1.GenesisState</a></td>
                  <td></td>
                  <td><p>ICS004 - Channel genesis state </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/lightclients/localhost/v1/localhost.proto">ibcgo/lightclients/localhost/v1/localhost.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.lightclients.localhost.v1.ClientState">ClientState</h3>
        <p>ClientState defines a loopback (localhost) client. It requires (read-only)</p><p>access to keys outside the client prefix.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>chain_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>self chain ID </p></td>
                </tr>
              
                <tr>
                  <td>height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>self latest block height </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/lightclients/solomachine/v1/solomachine.proto">ibcgo/lightclients/solomachine/v1/solomachine.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.lightclients.solomachine.v1.ChannelStateData">ChannelStateData</h3>
        <p>ChannelStateData returns the SignBytes data for channel state</p><p>verification.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>path</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>channel</td>
                  <td><a href="#ibcgo.core.channel.v1.Channel">ibcgo.core.channel.v1.Channel</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.ClientState">ClientState</h3>
        <p>ClientState defines a solo machine client that tracks the current consensus</p><p>state and if the client is frozen.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>latest sequence of the client state </p></td>
                </tr>
              
                <tr>
                  <td>frozen_sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>frozen sequence of the solo machine </p></td>
                </tr>
              
                <tr>
                  <td>consensus_state</td>
                  <td><a href="#ibcgo.lightclients.solomachine.v1.ConsensusState">ConsensusState</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>allow_update_after_proposal</td>
                  <td><a href="#bool">bool</a></td>
                  <td></td>
                  <td><p>when set to true, will allow governance to update a solo machine client.
The client will be unfrozen if it is frozen. </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.ClientStateData">ClientStateData</h3>
        <p>ClientStateData returns the SignBytes data for client state verification.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>path</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>client_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.ConnectionStateData">ConnectionStateData</h3>
        <p>ConnectionStateData returns the SignBytes data for connection state</p><p>verification.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>path</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>connection</td>
                  <td><a href="#ibcgo.core.connection.v1.ConnectionEnd">ibcgo.core.connection.v1.ConnectionEnd</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.ConsensusState">ConsensusState</h3>
        <p>ConsensusState defines a solo machine consensus state. The sequence of a</p><p>consensus state is contained in the "height" key used in storing the</p><p>consensus state.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>public_key</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>public key of the solo machine </p></td>
                </tr>
              
                <tr>
                  <td>diversifier</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>diversifier allows the same public key to be re-used across different solo
machine clients (potentially on different chains) without being considered
misbehaviour. </p></td>
                </tr>
              
                <tr>
                  <td>timestamp</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.ConsensusStateData">ConsensusStateData</h3>
        <p>ConsensusStateData returns the SignBytes data for consensus state</p><p>verification.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>path</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>consensus_state</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.Header">Header</h3>
        <p>Header defines a solo machine consensus header</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p>sequence to update solo machine public key at </p></td>
                </tr>
              
                <tr>
                  <td>timestamp</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signature</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>new_public_key</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>new_diversifier</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.HeaderData">HeaderData</h3>
        <p>HeaderData returns the SignBytes data for update verification.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>new_pub_key</td>
                  <td><a href="#google.protobuf.Any">google.protobuf.Any</a></td>
                  <td></td>
                  <td><p>header public key </p></td>
                </tr>
              
                <tr>
                  <td>new_diversifier</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p>header diversifier </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.Misbehaviour">Misbehaviour</h3>
        <p>Misbehaviour defines misbehaviour for a solo machine which consists</p><p>of a sequence and two signatures over different messages at that sequence.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signature_one</td>
                  <td><a href="#ibcgo.lightclients.solomachine.v1.SignatureAndData">SignatureAndData</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>signature_two</td>
                  <td><a href="#ibcgo.lightclients.solomachine.v1.SignatureAndData">SignatureAndData</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.NextSequenceRecvData">NextSequenceRecvData</h3>
        <p>NextSequenceRecvData returns the SignBytes data for verification of the next</p><p>sequence to be received.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>path</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>next_seq_recv</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.PacketAcknowledgementData">PacketAcknowledgementData</h3>
        <p>PacketAcknowledgementData returns the SignBytes data for acknowledgement</p><p>verification.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>path</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>acknowledgement</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.PacketCommitmentData">PacketCommitmentData</h3>
        <p>PacketCommitmentData returns the SignBytes data for packet commitment</p><p>verification.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>path</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>commitment</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.PacketReceiptAbsenceData">PacketReceiptAbsenceData</h3>
        <p>PacketReceiptAbsenceData returns the SignBytes data for</p><p>packet receipt absence verification.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>path</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.SignBytes">SignBytes</h3>
        <p>SignBytes defines the signed bytes used for signature verification.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>sequence</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>timestamp</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>diversifier</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>data_type</td>
                  <td><a href="#ibcgo.lightclients.solomachine.v1.DataType">DataType</a></td>
                  <td></td>
                  <td><p>type of the data used </p></td>
                </tr>
              
                <tr>
                  <td>data</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p>marshaled data </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.SignatureAndData">SignatureAndData</h3>
        <p>SignatureAndData contains a signature and the data signed over to create that</p><p>signature.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>signature</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>data_type</td>
                  <td><a href="#ibcgo.lightclients.solomachine.v1.DataType">DataType</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>data</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>timestamp</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.solomachine.v1.TimestampedSignatureData">TimestampedSignatureData</h3>
        <p>TimestampedSignatureData contains the signature data and the timestamp of the</p><p>signature.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>signature_data</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>timestamp</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      
        <h3 id="ibcgo.lightclients.solomachine.v1.DataType">DataType</h3>
        <p>DataType defines the type of solo machine proof being created. This is done</p><p>to preserve uniqueness of different data sign byte encodings.</p>
        <table class="enum-table">
          <thead>
            <tr><td>Name</td><td>Number</td><td>Description</td></tr>
          </thead>
          <tbody>
            
              <tr>
                <td>DATA_TYPE_UNINITIALIZED_UNSPECIFIED</td>
                <td>0</td>
                <td><p>Default State</p></td>
              </tr>
            
              <tr>
                <td>DATA_TYPE_CLIENT_STATE</td>
                <td>1</td>
                <td><p>Data type for client state verification</p></td>
              </tr>
            
              <tr>
                <td>DATA_TYPE_CONSENSUS_STATE</td>
                <td>2</td>
                <td><p>Data type for consensus state verification</p></td>
              </tr>
            
              <tr>
                <td>DATA_TYPE_CONNECTION_STATE</td>
                <td>3</td>
                <td><p>Data type for connection state verification</p></td>
              </tr>
            
              <tr>
                <td>DATA_TYPE_CHANNEL_STATE</td>
                <td>4</td>
                <td><p>Data type for channel state verification</p></td>
              </tr>
            
              <tr>
                <td>DATA_TYPE_PACKET_COMMITMENT</td>
                <td>5</td>
                <td><p>Data type for packet commitment verification</p></td>
              </tr>
            
              <tr>
                <td>DATA_TYPE_PACKET_ACKNOWLEDGEMENT</td>
                <td>6</td>
                <td><p>Data type for packet acknowledgement verification</p></td>
              </tr>
            
              <tr>
                <td>DATA_TYPE_PACKET_RECEIPT_ABSENCE</td>
                <td>7</td>
                <td><p>Data type for packet receipt absence verification</p></td>
              </tr>
            
              <tr>
                <td>DATA_TYPE_NEXT_SEQUENCE_RECV</td>
                <td>8</td>
                <td><p>Data type for next sequence recv verification</p></td>
              </tr>
            
              <tr>
                <td>DATA_TYPE_HEADER</td>
                <td>9</td>
                <td><p>Data type for header verification</p></td>
              </tr>
            
          </tbody>
        </table>
      

      

      
    
      
      <div class="file-heading">
        <h2 id="ibcgo/lightclients/tendermint/v1/tendermint.proto">ibcgo/lightclients/tendermint/v1/tendermint.proto</h2><a href="#title">Top</a>
      </div>
      <p></p>

      
        <h3 id="ibcgo.lightclients.tendermint.v1.ClientState">ClientState</h3>
        <p>ClientState from Tendermint tracks the current validator set, latest height,</p><p>and a possible frozen height.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>chain_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>trust_level</td>
                  <td><a href="#ibcgo.lightclients.tendermint.v1.Fraction">Fraction</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>trusting_period</td>
                  <td><a href="#google.protobuf.Duration">google.protobuf.Duration</a></td>
                  <td></td>
                  <td><p>duration of the period since the LastestTimestamp during which the
submitted headers are valid for upgrade </p></td>
                </tr>
              
                <tr>
                  <td>unbonding_period</td>
                  <td><a href="#google.protobuf.Duration">google.protobuf.Duration</a></td>
                  <td></td>
                  <td><p>duration of the staking unbonding period </p></td>
                </tr>
              
                <tr>
                  <td>max_clock_drift</td>
                  <td><a href="#google.protobuf.Duration">google.protobuf.Duration</a></td>
                  <td></td>
                  <td><p>defines how much new (untrusted) header&#39;s Time can drift into the future. </p></td>
                </tr>
              
                <tr>
                  <td>frozen_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>Block height when the client was frozen due to a misbehaviour </p></td>
                </tr>
              
                <tr>
                  <td>latest_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p>Latest height the client was updated to </p></td>
                </tr>
              
                <tr>
                  <td>proof_specs</td>
                  <td><a href="#ics23.ProofSpec">ics23.ProofSpec</a></td>
                  <td>repeated</td>
                  <td><p>Proof specifications used in verifying counterparty state </p></td>
                </tr>
              
                <tr>
                  <td>upgrade_path</td>
                  <td><a href="#string">string</a></td>
                  <td>repeated</td>
                  <td><p>Path at which next upgraded client will be committed.
Each element corresponds to the key for a single CommitmentProof in the
chained proof. NOTE: ClientState must stored under
`{upgradePath}/{upgradeHeight}/clientState` ConsensusState must be stored
under `{upgradepath}/{upgradeHeight}/consensusState` For SDK chains using
the default upgrade module, upgrade_path should be []string{&#34;upgrade&#34;,
&#34;upgradedIBCState&#34;}` </p></td>
                </tr>
              
                <tr>
                  <td>allow_update_after_expiry</td>
                  <td><a href="#bool">bool</a></td>
                  <td></td>
                  <td><p>This flag, when set to true, will allow governance to recover a client
which has expired </p></td>
                </tr>
              
                <tr>
                  <td>allow_update_after_misbehaviour</td>
                  <td><a href="#bool">bool</a></td>
                  <td></td>
                  <td><p>This flag, when set to true, will allow governance to unfreeze a client
whose chain has experienced a misbehaviour event </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.tendermint.v1.ConsensusState">ConsensusState</h3>
        <p>ConsensusState defines the consensus state from Tendermint.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>timestamp</td>
                  <td><a href="#google.protobuf.Timestamp">google.protobuf.Timestamp</a></td>
                  <td></td>
                  <td><p>timestamp that corresponds to the block height in which the ConsensusState
was stored. </p></td>
                </tr>
              
                <tr>
                  <td>root</td>
                  <td><a href="#ibcgo.core.commitment.v1.MerkleRoot">ibcgo.core.commitment.v1.MerkleRoot</a></td>
                  <td></td>
                  <td><p>commitment root (i.e app hash) </p></td>
                </tr>
              
                <tr>
                  <td>next_validators_hash</td>
                  <td><a href="#bytes">bytes</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.tendermint.v1.Fraction">Fraction</h3>
        <p>Fraction defines the protobuf message type for tmmath.Fraction that only</p><p>supports positive values.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>numerator</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>denominator</td>
                  <td><a href="#uint64">uint64</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.tendermint.v1.Header">Header</h3>
        <p>Header defines the Tendermint client consensus Header.</p><p>It encapsulates all the information necessary to update from a trusted</p><p>Tendermint ConsensusState. The inclusion of TrustedHeight and</p><p>TrustedValidators allows this update to process correctly, so long as the</p><p>ConsensusState for the TrustedHeight exists, this removes race conditions</p><p>among relayers The SignedHeader and ValidatorSet are the new untrusted update</p><p>fields for the client. The TrustedHeight is the height of a stored</p><p>ConsensusState on the client that will be used to verify the new untrusted</p><p>header. The Trusted ConsensusState must be within the unbonding period of</p><p>current time in order to correctly verify, and the TrustedValidators must</p><p>hash to TrustedConsensusState.NextValidatorsHash since that is the last</p><p>trusted validator set at the TrustedHeight.</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>signed_header</td>
                  <td><a href="#tendermint.types.SignedHeader">tendermint.types.SignedHeader</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>validator_set</td>
                  <td><a href="#tendermint.types.ValidatorSet">tendermint.types.ValidatorSet</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>trusted_height</td>
                  <td><a href="#ibcgo.core.client.v1.Height">ibcgo.core.client.v1.Height</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>trusted_validators</td>
                  <td><a href="#tendermint.types.ValidatorSet">tendermint.types.ValidatorSet</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      
        <h3 id="ibcgo.lightclients.tendermint.v1.Misbehaviour">Misbehaviour</h3>
        <p>Misbehaviour is a wrapper over two conflicting Headers</p><p>that implements Misbehaviour interface expected by ICS-02</p>

        
          <table class="field-table">
            <thead>
              <tr><td>Field</td><td>Type</td><td>Label</td><td>Description</td></tr>
            </thead>
            <tbody>
              
                <tr>
                  <td>client_id</td>
                  <td><a href="#string">string</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>header_1</td>
                  <td><a href="#ibcgo.lightclients.tendermint.v1.Header">Header</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
                <tr>
                  <td>header_2</td>
                  <td><a href="#ibcgo.lightclients.tendermint.v1.Header">Header</a></td>
                  <td></td>
                  <td><p> </p></td>
                </tr>
              
            </tbody>
          </table>

          

        
      

      

      

      
    

    <h2 id="scalar-value-types">Scalar Value Types</h2>
    <table class="scalar-value-types-table">
      <thead>
        <tr><td>.proto Type</td><td>Notes</td><td>C++</td><td>Java</td><td>Python</td><td>Go</td><td>C#</td><td>PHP</td><td>Ruby</td></tr>
      </thead>
      <tbody>
        
          <tr id="double">
            <td>double</td>
            <td></td>
            <td>double</td>
            <td>double</td>
            <td>float</td>
            <td>float64</td>
            <td>double</td>
            <td>float</td>
            <td>Float</td>
          </tr>
        
          <tr id="float">
            <td>float</td>
            <td></td>
            <td>float</td>
            <td>float</td>
            <td>float</td>
            <td>float32</td>
            <td>float</td>
            <td>float</td>
            <td>Float</td>
          </tr>
        
          <tr id="int32">
            <td>int32</td>
            <td>Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead.</td>
            <td>int32</td>
            <td>int</td>
            <td>int</td>
            <td>int32</td>
            <td>int</td>
            <td>integer</td>
            <td>Bignum or Fixnum (as required)</td>
          </tr>
        
          <tr id="int64">
            <td>int64</td>
            <td>Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead.</td>
            <td>int64</td>
            <td>long</td>
            <td>int/long</td>
            <td>int64</td>
            <td>long</td>
            <td>integer/string</td>
            <td>Bignum</td>
          </tr>
        
          <tr id="uint32">
            <td>uint32</td>
            <td>Uses variable-length encoding.</td>
            <td>uint32</td>
            <td>int</td>
            <td>int/long</td>
            <td>uint32</td>
            <td>uint</td>
            <td>integer</td>
            <td>Bignum or Fixnum (as required)</td>
          </tr>
        
          <tr id="uint64">
            <td>uint64</td>
            <td>Uses variable-length encoding.</td>
            <td>uint64</td>
            <td>long</td>
            <td>int/long</td>
            <td>uint64</td>
            <td>ulong</td>
            <td>integer/string</td>
            <td>Bignum or Fixnum (as required)</td>
          </tr>
        
          <tr id="sint32">
            <td>sint32</td>
            <td>Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s.</td>
            <td>int32</td>
            <td>int</td>
            <td>int</td>
            <td>int32</td>
            <td>int</td>
            <td>integer</td>
            <td>Bignum or Fixnum (as required)</td>
          </tr>
        
          <tr id="sint64">
            <td>sint64</td>
            <td>Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s.</td>
            <td>int64</td>
            <td>long</td>
            <td>int/long</td>
            <td>int64</td>
            <td>long</td>
            <td>integer/string</td>
            <td>Bignum</td>
          </tr>
        
          <tr id="fixed32">
            <td>fixed32</td>
            <td>Always four bytes. More efficient than uint32 if values are often greater than 2^28.</td>
            <td>uint32</td>
            <td>int</td>
            <td>int</td>
            <td>uint32</td>
            <td>uint</td>
            <td>integer</td>
            <td>Bignum or Fixnum (as required)</td>
          </tr>
        
          <tr id="fixed64">
            <td>fixed64</td>
            <td>Always eight bytes. More efficient than uint64 if values are often greater than 2^56.</td>
            <td>uint64</td>
            <td>long</td>
            <td>int/long</td>
            <td>uint64</td>
            <td>ulong</td>
            <td>integer/string</td>
            <td>Bignum</td>
          </tr>
        
          <tr id="sfixed32">
            <td>sfixed32</td>
            <td>Always four bytes.</td>
            <td>int32</td>
            <td>int</td>
            <td>int</td>
            <td>int32</td>
            <td>int</td>
            <td>integer</td>
            <td>Bignum or Fixnum (as required)</td>
          </tr>
        
          <tr id="sfixed64">
            <td>sfixed64</td>
            <td>Always eight bytes.</td>
            <td>int64</td>
            <td>long</td>
            <td>int/long</td>
            <td>int64</td>
            <td>long</td>
            <td>integer/string</td>
            <td>Bignum</td>
          </tr>
        
          <tr id="bool">
            <td>bool</td>
            <td></td>
            <td>bool</td>
            <td>boolean</td>
            <td>boolean</td>
            <td>bool</td>
            <td>bool</td>
            <td>boolean</td>
            <td>TrueClass/FalseClass</td>
          </tr>
        
          <tr id="string">
            <td>string</td>
            <td>A string must always contain UTF-8 encoded or 7-bit ASCII text.</td>
            <td>string</td>
            <td>String</td>
            <td>str/unicode</td>
            <td>string</td>
            <td>string</td>
            <td>string</td>
            <td>String (UTF-8)</td>
          </tr>
        
          <tr id="bytes">
            <td>bytes</td>
            <td>May contain any arbitrary sequence of bytes.</td>
            <td>string</td>
            <td>ByteString</td>
            <td>str</td>
            <td>[]byte</td>
            <td>ByteString</td>
            <td>string</td>
            <td>String (ASCII-8BIT)</td>
          </tr>
        
      </tbody>
    </table>
  </body>
</html>

