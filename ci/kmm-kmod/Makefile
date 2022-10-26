ifndef KERNEL_SRC_DIR
KERNEL_SRC_DIR =/lib/modules/$(shell uname -r)/build
endif

ifndef MY_MODULE
EXTRA_MODULE=kmm_ci_b.o
endif

ccflags-y += -I$(KERNEL_SRC_DIR)/include
obj-m += kmm_ci_a.o
obj-m += $(EXTRA_MODULE)

all:
	make -C $(KERNEL_SRC_DIR) $(MAKE_OPTS) M=$(PWD) modules

