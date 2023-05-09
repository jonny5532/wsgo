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
		wsgo.notify_retry("12345")
		start_response('200 OK', [
			('Content-Type','text/html'),
		])
		return [b"notified!"]

	time.sleep(1)

	print('env is', env)

	start_response('200 OK', [
		('Content-Type','text/html'),
		('X-WSGo-Retry','12345'),
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
