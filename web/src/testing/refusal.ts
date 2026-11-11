// A server's refusal as the socket raises it: the code, worded by the client.
import { RequestError } from '@/net/socket';
import { errorText } from '@/protocol/errors';
import type { ErrorCode } from '@/protocol/errors';

export function refusal(code: ErrorCode): RequestError {
  return new RequestError(code, errorText(code));
}
