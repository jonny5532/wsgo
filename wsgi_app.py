import wsgo

import threading
import time

lock = threading.Lock()

#@wsgo.cron(21, 7, -1, -1, -1)
# @wsgo.timer(15)
# def tick_test():
# 	print("tick")
# 	print("acquiring lock")
# 	lock.acquire()
# 	print("got lock")


def application(env, start_response):
	if env['PATH_INFO']=='/notify':
		wsgo.notify_parked("12345", wsgo.RETRY, "boo")
		start_response('200 OK', [
			('Content-Type','text/html'),
		])
		return [b"notified!"]

	if env['PATH_INFO']=='/data':
		start_response('200 OK', [
			('Content-Type','text/html'),
		])
		return [b"0123456789"*100]

	if env['PATH_INFO']=='/favicon.ico':
		start_response('404 Not Found', [
			('Content-Type','text/html'),
		])
		return [b"Not found"]

	if env['PATH_INFO']=='/wait':
		time.sleep(10)
		start_response('200 OK', [
			('Content-Type','text/html'),
		])
		return [b"hiya"]

	time.sleep(1)

	print('env is', env)

	retry = []
	if not 'HTTP_X_WSGO_PARK_ARG' in env:
		retry = [
			('X-WSGo-Park', '12345, 12346'),
			('X-WSGo-Park-Timeout', '6 http-204'),
		]


	start_response('200 OK', [
		('Content-Type','text/html'),
		*retry
	])
	# for i in range(100000):
	# 	x = i+2
	# time.sleep(1)
	print('gonna return hello world?')
	return [b"Hello World"]

@wsgo.timer(10)
def sleep_test():
	print("sleep start")
	time.sleep(5)
	print("sleep end")
