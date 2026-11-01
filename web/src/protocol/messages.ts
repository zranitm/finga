// The messages are generated from server/api/proto/client/v1/client.proto by npm run gen.
import type { MessageInitShape } from '@bufbuild/protobuf';

import type { ClientMessage, ClientMessageSchema, ServerMessage } from './gen/client/v1/client_pb';

export * from './gen/client/v1/client_pb';

/** A server message's body: the case names the message, the value carries it. */
export type ServerBody = Exclude<ServerMessage['body'], { case: undefined }>;
export type MessageType = ServerBody['case'];
export type Data<T extends MessageType> = Extract<ServerBody, { case: T }>['value'];

export type ClientBody = Exclude<ClientMessage['body'], { case: undefined }>;
export type ClientMessageType = ClientBody['case'];

export type ClientBodyInit = Extract<
  MessageInitShape<typeof ClientMessageSchema>['body'],
  { case: ClientMessageType }
>;
/** What a sender passes for a client message: any field left out is its default. */
export type ClientInit<T extends ClientMessageType> = Extract<ClientBodyInit, { case: T }>['value'];
