-- Synthetic Messages schema. No personal data; also usable by the real API/TUI.
CREATE TABLE chat (ROWID INTEGER PRIMARY KEY, guid TEXT, display_name TEXT, service_name TEXT, chat_identifier TEXT, group_id TEXT, is_archived INTEGER, is_deleted INTEGER);
CREATE TABLE message (ROWID INTEGER PRIMARY KEY, guid TEXT, text TEXT, attributedBody BLOB, date INTEGER, is_from_me INTEGER, handle_id INTEGER, service TEXT, item_type INTEGER, group_action_type INTEGER, associated_message_type INTEGER, is_deleted INTEGER);
CREATE TABLE chat_message_join (chat_id INTEGER, message_id INTEGER);
CREATE TABLE chat_handle_join (chat_id INTEGER, handle_id INTEGER);
CREATE TABLE handle (ROWID INTEGER PRIMARY KEY, id TEXT);
CREATE TABLE attachment (ROWID INTEGER PRIMARY KEY, guid TEXT, filename TEXT, transfer_name TEXT, mime_type TEXT);
CREATE TABLE message_attachment_join (message_id INTEGER, attachment_id INTEGER);
INSERT INTO chat VALUES
 (1, 'iMessage;-;alex@example.invalid', 'Stale name', 'iMessage', 'alex@example.invalid', NULL, 0, 0),
 (2, 'iMessage;+;weekend', '', 'iMessage', 'weekend', 'group-pin', 0, 0),
 (3, 'any;-;+1 (415) 555-0100', NULL, 'SMS', '+1 (415) 555-0100', NULL, 0, 0),
 (4, 'iMessage;+;empty-group', '  ', NULL, NULL, NULL, 0, 0),
 (5, 'iMessage;-;archived', 'Archived', NULL, NULL, NULL, 1, 0),
 (6, 'iMessage;-;deleted', 'Deleted', NULL, NULL, NULL, 0, 1);
INSERT INTO handle VALUES (1, 'alex@example.invalid'), (2, '+1 (415) 555-0100'), (3, 'unknown@example.invalid');
INSERT INTO chat_handle_join VALUES (1,1), (2,1), (2,2), (2,3), (2,1), (3,2);
INSERT INTO message VALUES
 (1, 'first', 'Hello from the fixture 👋', NULL, 800000000000000000, 0, 1, 'iMessage', 0, 0, 0, 0),
 (2, 'second', 'Sent from the fixture.', NULL, 800000001000000000, 1, NULL, 'iMessage', 0, 0, 0, 0),
 (3, 'archived-body', NULL, X'040b73747265616d747970656481e803840140848484124e5341747472696275746564537472696e67008484084e534f626a656374008592848484084e53537472696e67019484012b104172636869766564206d65737361676586840269490110928484840c4e5344696374696f6e6172790094840169008686', 800000002000000000, 0, 1, 'iMessage', 0, 0, 0, 0),
 (4, 'reaction', 'Liked a message', NULL, 900000000000000000, 0, 1, 'iMessage', 0, 0, 2000, 0),
 (5, 'action', 'Joined', NULL, 900000000000000000, 0, 1, 'iMessage', 0, 1, 0, 0),
 (6, 'deleted', 'Deleted', NULL, 900000000000000000, 0, 1, 'iMessage', 0, 0, 0, 1),
 (7, 'item', 'System item', NULL, 900000000000000000, 0, 1, 'iMessage', 1, 0, 0, 0),
 (8, 'invalid-body', NULL, X'010203', 800000002000000000, 0, 1, 'iMessage', 0, 0, 0, 0),
 (9, 'empty-body', NULL, X'', 799999999000000000, 0, 1, 'iMessage', 0, 0, 0, 0),
 (10, 'group', 'Saturday at ten?', NULL, 700000000000000000, 0, 2, 'iMessage', 0, 0, 0, 0),
 (11, 'phone', 'Sounds good.', NULL, 600000000000000000, 0, 2, 'SMS', 0, 0, 0, 0),
 (12, 'empty-group', '', NULL, 500000000000000000, 0, NULL, NULL, 0, 0, 0, 0),
 (13, 'hidden', 'Hidden chat', NULL, 900000000000000000, 0, NULL, NULL, 0, 0, 0, 0),
 (14, 'attachment-only', NULL, NULL, 800000003000000000, 0, 1, 'iMessage', 0, 0, 0, 0),
 (15, 'no-content', NULL, NULL, 900000000000000000, 0, 1, NULL, 0, 0, 0, 0),
 (16, 'no-date', 'No date', NULL, NULL, 0, 1, NULL, 0, 0, 0, 0);
INSERT INTO chat_message_join VALUES (1,1),(1,2),(1,3),(1,4),(1,5),(1,6),(1,7),(1,8),(1,9),(2,10),(3,11),(4,12),(5,13),(6,13),(1,14),(1,15),(1,16);
INSERT INTO attachment VALUES (1, 'photo', NULL, 'fixture.png', 'image/png');
INSERT INTO message_attachment_join VALUES (14,1);
ALTER TABLE message ADD COLUMN is_read INTEGER DEFAULT 1;
UPDATE message SET is_read=0 WHERE ROWID IN (2,3,4,5,6,7,10,14);
